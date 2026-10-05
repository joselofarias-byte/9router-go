package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go.uber.org/fx"

	"9router/proxy/internal/config"
	"9router/proxy/internal/controlplane/discovery"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
	"9router/proxy/internal/proxy/oauth"
	"9router/proxy/internal/shutdown"
	"9router/proxy/internal/updater"
)

// ServerModule provides *http.Server and manages its lifecycle and background tasks.
var ServerModule = fx.Module("server",
	fx.Provide(
		ProvideServer,
	),
	fx.Invoke(
		func(*http.Server) {},
	),
)

// ServerParams defines inputs for constructing the HTTP server and lifecycle hooks.
type ServerParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Config    *config.Config
	DB        *sql.DB
	Repo      *db.Repo
	Handler   http.Handler
	CLIParams CLIParams
}

// ProvideServer creates *http.Server and registers lifecycle hooks.
func ProvideServer(p ServerParams) *http.Server {
	var addr string
	if p.Config.Host != "" {
		addr = net.JoinHostPort(p.Config.Host, strconv.Itoa(p.Config.Port))
	} else {
		addr = fmt.Sprintf(":%d", p.Config.Port)
	}
	server := &http.Server{
		Addr:    addr,
		Handler: p.Handler,
	}

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// The modular Fx startup replaced the legacy main() bootstrap in
			// upstream-reconcile. Keep Fabric's registry alive before the first
			// request; otherwise free/free-best sees a nil state even though the
			// ordinary model catalog has already synchronized.
			if err := registry.InitRegistry(p.DB); err != nil {
				log.Printf("[config] registry init warning: %v", err)
			}

			// Seed the compiled zero-credential free lanes immediately. The
			// network discovery orchestrator intentionally waits before its
			// first pass; without this local-only seed a fresh process would
			// return free_route_unavailable during that startup window.
			discovery.NewOrchestrator(p.DB, []discovery.Adapter{
				discovery.NewBuiltinFreeAdapter(),
			}).RunSync(ctx)

			adapters := []discovery.Adapter{
				discovery.NewModelsDevAdapter(nil),
				discovery.NewOpenRouterAdapter(nil),
				discovery.NewLlamaCppAdapter(nil, os.Getenv("LLAMACPP_BASE_URL")),
				discovery.NewClineFreeAdapter(nil),
			}
			if unoAPIKey := os.Getenv("UNOROUTER_API_KEY"); unoAPIKey != "" {
				adapters = append(adapters, discovery.NewUnoRouterAdapter(nil, unoAPIKey))
			} else {
				log.Printf("[config] unorouter adapter skipped (UNOROUTER_API_KEY not set)")
			}
			if orcaBaseURL, orcaAPIKey := os.Getenv("ORCAROUTER_BASE_URL"), os.Getenv("ORCAROUTER_API_KEY"); orcaBaseURL != "" && orcaAPIKey != "" {
				adapters = append(adapters, discovery.NewOrcaRouterAdapter(nil, orcaBaseURL, orcaAPIKey))
			}
			orchestrator := discovery.NewOrchestrator(p.DB, adapters)
			orchestrator.Start(shutdown.Context())

			autoUpdate := p.CLIParams.AutoUpdate
			if !autoUpdate && p.Repo != nil {
				if settings, sErr := p.Repo.GetSettings(); sErr == nil && settings != nil {
					autoUpdate = settings.AutoUpdate
				}
			}
			updater.StartBackgroundCheck(shutdown.Context(), autoUpdate)
			log.Printf("[config] auto-update enabled=%v", autoUpdate)

			catalogPath := filepath.Join(filepath.Dir(p.Config.DatabasePath), "model-catalog.json")
			providers.StartBackgroundCatalogSync(shutdown.Context(), nil, catalogPath)
			oauth.StartBackgroundRefresh(shutdown.Context(), p.Repo)

			log.Printf("9router-go Proxy (%s) starting on port %d", updater.CurrentVersion, p.Config.Port)

			go func() {
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatalf("Server failed: %v", err)
				}
			}()

			fmt.Fprintf(os.Stdout, "\n  🚀 9router-go Proxy (%s) on %s\n\n", updater.CurrentVersion, addr)
			log.Printf("Server is ready to handle requests at %s", addr)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			fmt.Fprintln(os.Stdout, "\n  Shutting down...")

			// Signal in-flight SSE streams to end promptly: the stall reader closes each
			// upstream body, handlers emit a final [DONE], and Shutdown completes well
			// within its deadline instead of waiting out the full timeout.
			shutdown.Cancel()

			// Refuse NEW keep-alive reuse immediately: idle browser/dashboard
			// connections otherwise hold Shutdown for the full deadline even
			// when zero requests are in flight (the "shutdown takes forever"
			// complaint). In-flight requests still drain gracefully below.
			server.SetKeepAlivesEnabled(false)

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				log.Printf("Server shutdown did not complete in time: %v", err)
			} else {
				log.Println("Server stopped gracefully")
			}
			return nil
		},
	})

	return server
}
