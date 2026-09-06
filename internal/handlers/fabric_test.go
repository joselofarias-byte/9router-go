package handlers

import (
	"9router/proxy/internal/db"
	"9router/proxy/internal/dbtest"
	"bytes"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFabricRoutesAuthAndRedaction(t *testing.T) {
	d, close := setupTestDB(t)
	defer close()
	if e := dbtest.CreateTables(d); e != nil {
		t.Fatal(e)
	}
	d.Exec(`INSERT INTO apiKeys(id,key,isActive,createdAt) VALUES('client','inference-only',1,'')`)
	d.Exec(`INSERT INTO providerConnections(id,provider,authType,data,createdAt,updatedAt) VALUES('account','fabric-test','apikey','{"apiKey":"SECRET_SENTINEL"}','','')`)
	token := strings.Repeat("a", 32)
	t.Setenv("NINEROUTER_ADMIN_TOKEN", token)
	r := chi.NewRouter()
	SetupServerRouter(r, db.NewRepo(d), nil)
	for _, test := range []struct {
		path, key string
		code      int
	}{{"/admin/fabric/state", "inference-only", 401}, {"/admin/fabric/accounts", token, 200}, {"/models", token, 401}, {"/health", "", 200}} {
		q := httptest.NewRequest("GET", test.path, nil)
		q.Header.Set("Authorization", "Bearer "+test.key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		if w.Code != test.code {
			t.Fatalf("%s: %d %s", test.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "SECRET_SENTINEL") {
			t.Fatal("secret exposed")
		}
	}
	raw, _ := json.Marshal(map[string]any{"expectedHash": "", "snapshot": nil})
	q := httptest.NewRequest("POST", "/admin/fabric/apply", bytes.NewReader(raw))
	q.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	if w.Code != http.StatusBadRequest {
		t.Fatal("invalid apply accepted")
	}
}

func TestFabricRotationPreservesSettings(t *testing.T) {
	d, close := setupTestDB(t)
	defer close()
	if err := dbtest.CreateTables(d); err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO providerConnections(id,provider,authType,data,createdAt,updatedAt) VALUES('a','fabric-test','apikey','{}','','')`)
	d.Exec(`INSERT INTO settings(id,data) VALUES(1,'{"customOption":true}')`)
	repo := db.NewRepo(d)
	req := httptest.NewRequest("POST", "/admin/fabric/rotation", strings.NewReader(`{"provider":"fabric-test","strategy":"round-robin"}`))
	w := httptest.NewRecorder()
	fabricRotation(repo)(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	settings, err := repo.GetSettings()
	if err != nil || settings.ProviderStrategies["fabric-test"].RotateStrategy != "round-robin" {
		t.Fatalf("policy not stored: %+v %v", settings, err)
	}
	var kept int
	if err := d.QueryRow(`SELECT json_extract(data,'$.customOption') FROM settings`).Scan(&kept); err != nil || kept != 1 {
		t.Fatal("unrelated settings overwritten")
	}
}
