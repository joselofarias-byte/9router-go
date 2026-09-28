package app

import (
	"context"
	"database/sql"
	"os"

	"go.uber.org/fx"

	"9router/proxy/internal/controlplane/discovery"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/log"
)

// FabricModule wires the fork control plane into the current Fx lifecycle
// without replacing upstream data-plane components.
var FabricModule = fx.Module("fabric",
	fx.Invoke(StartFabric),
)

// StartFabric initializes the persisted registry and starts conservative
// background discovery. Adapters that require credentials remain opt-in.
func StartFabric(lc fx.Lifecycle, conn *sql.DB) {
	fabricCtx, cancel := context.WithCancel(context.Background())

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := registry.InitRegistry(conn); err != nil {
				// Fabric must not prevent the upstream proxy from starting.
				// An empty registry fails closed for free routes until discovery
				// can establish a valid snapshot.
				log.Warn("fabric", "registry initialization failed; starting with fail-closed free routing", "error", err)
			}

			adapters := []discovery.Adapter{
				discovery.NewModelsDevAdapter(nil),
				discovery.NewClineFreeAdapter(nil),
				discovery.NewKiraAdapter(nil, os.Getenv("KIRA_BASE_URL")),
				discovery.NewKiroStaticAdapter(),
			}

			if apiKey := os.Getenv("UNOROUTER_API_KEY"); apiKey != "" {
				adapters = append(adapters, discovery.NewUnoRouterAdapter(nil, apiKey))
			}

			if apiKey := os.Getenv("APINEX_API_KEY"); apiKey != "" {
				adapters = append(adapters, discovery.NewAPInexAdapter(nil, os.Getenv("APINEX_BASE_URL"), apiKey))
			}

			if baseURL, apiKey := os.Getenv("ORCAROUTER_BASE_URL"), os.Getenv("ORCAROUTER_API_KEY"); baseURL != "" && apiKey != "" {
				adapters = append(adapters, discovery.NewOrcaRouterAdapter(nil, baseURL, apiKey))
			}

			discovery.NewOrchestrator(conn, adapters).Start(fabricCtx)
			log.Info("fabric", "control plane started", "adapters", len(adapters))
			return nil
		},
		OnStop: func(ctx context.Context) error {
			cancel()
			return nil
		},
	})
}
