package middleware

import (
	"9router/proxy/internal/db"
	"9router/proxy/internal/handlerutil"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// RequireAdmin is deliberately independent of inference API keys and disabled
// until a separate operator token is supplied through the process environment.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := os.Getenv("NINEROUTER_ADMIN_TOKEN")
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		a, b := sha256.Sum256([]byte(expected)), sha256.Sum256([]byte(provided))
		if len(expected) < 32 || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
			handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Administrative authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireGatewayAuth(repo *db.Repo) func(http.Handler) http.Handler {
	inference := map[string]bool{
		"/chat/completions": true, "/messages": true, "/messages/count_tokens": true,
		"/api/chat": true, "/embeddings": true, "/responses": true, "/responses/compact": true,
		"/images/generations": true, "/audio/speech": true, "/audio/voices": true, "/audio/transcriptions": true,
		"/videos/generations": true, "/videos/edits": true, "/videos/extensions": true,
		"/search": true, "/scrape": true, "/web/fetch": true, "/models": true, "/models/info": true,
		"/version": true, "/api/version": true,
	}
	return func(next http.Handler) http.Handler {
		admin, engine := RequireAdmin(next), RequireApiKey(repo)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if inference[p] || strings.HasPrefix(p, "/models/") || (r.Method == http.MethodGet && strings.HasPrefix(p, "/videos/")) {
				engine.ServeHTTP(w, r)
			} else {
				admin.ServeHTTP(w, r)
			}
		})
	}
}
