package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/web"
)

func TestHandler_WebAssetsAndPWARetirement(t *testing.T) {
	handler := web.Handler()

	tests := []struct {
		name               string
		targetPath         string
		expectedStatus     int
		expectedHeaderKey  string
		expectedHeaderPart string
	}{
		{
			name:           "manifest.webmanifest is retired",
			targetPath:     "/manifest.webmanifest",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "manifest.json is retired",
			targetPath:     "/manifest.json",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "service worker is retired",
			targetPath:     "/sw.js",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:               "serves icon-192.png",
			targetPath:         "/icons/icon-192.png",
			expectedStatus:     http.StatusOK,
			expectedHeaderKey:  "Content-Type",
			expectedHeaderPart: "image/png",
		},
		{
			name:               "serves icon-512.png",
			targetPath:         "/icons/icon-512.png",
			expectedStatus:     http.StatusOK,
			expectedHeaderKey:  "Content-Type",
			expectedHeaderPart: "image/png",
		},
		{
			name:               "SPA fallback to index.html on unknown route",
			targetPath:         "/combos",
			expectedStatus:     http.StatusOK,
			expectedHeaderKey:  "Content-Type",
			expectedHeaderPart: "text/html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.targetPath, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Fatalf("expected status %d for %q, got %d", tt.expectedStatus, tt.targetPath, rec.Code)
			}

			if tt.expectedHeaderKey != "" {
				val := rec.Header().Get(tt.expectedHeaderKey)
				if !strings.Contains(val, tt.expectedHeaderPart) {
					t.Errorf("expected header %s to contain %q for %q, got %q",
						tt.expectedHeaderKey, tt.expectedHeaderPart, tt.targetPath, val)
				}
			}
			if tt.targetPath == "/sw.js" && rec.Header().Get("Service-Worker-Allowed") != "" {
				t.Errorf("retired service worker route must not advertise Service-Worker-Allowed")
			}
		})
	}
}
