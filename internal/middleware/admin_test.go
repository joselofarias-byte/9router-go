package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFabricAdminIsolation(t *testing.T) {
	token := strings.Repeat("x", 32)
	t.Setenv("NINEROUTER_ADMIN_TOKEN", token)
	h := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, s := range []struct {
		key  string
		code int
	}{{"", 401}, {"inference-key", 401}, {token, 204}} {
		r := httptest.NewRequest("GET", "/admin/fabric/state", nil)
		r.Header.Set("Authorization", "Bearer "+s.key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != s.code {
			t.Fatal(w.Code)
		}
	}
	t.Setenv("NINEROUTER_ADMIN_TOKEN", "")
	r := httptest.NewRequest("GET", "/admin/fabric/state", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("admin without token")
	}
}
