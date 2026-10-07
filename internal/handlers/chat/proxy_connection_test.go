package chat

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/db"
)

func TestGetClientForConnection_ProxyPool(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}

	repo := db.NewRepo(database)
	pool, err := repo.InsertProxyPool(db.ProxyPoolData{
		Name:     "sg-proxy",
		ProxyURL: "http://user:pass@proxy.example.com:8080",
		Type:     "http",
	})
	if err != nil {
		t.Fatalf("failed to insert proxy pool: %v", err)
	}

	poolID := pool["id"].(string)

	h := &ChatHandler{
		Client: &http.Client{},
		Repo:   repo,
	}

	connData := &ConnectionData{
		ProxyPoolID: poolID,
	}

	client := h.GetClientForConnection(connData)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client == h.Client {
		t.Fatal("expected new client with custom proxy transport, got default client")
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatal("expected *http.Transport")
	}

	req, _ := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("proxy resolve error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://user:pass@proxy.example.com:8080" {
		t.Errorf("expected proxy URL http://user:pass@proxy.example.com:8080, got %v", proxyURL)
	}
}


func TestGetClientForConnection_StrictInvalidProxyFailsClosed(t *testing.T) {
	var directHits atomic.Int32
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer direct.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	h := &ChatHandler{Client: &http.Client{}, Repo: db.NewRepo(database)}

	connData := &ConnectionData{
		ConnectionProxyEnabled: true,
		ConnectionProxyURL:     "://invalid",
		StrictProxy:            true,
	}

	client := h.GetClientForConnection(connData)
	if client == h.Client {
		t.Fatal("strict invalid proxy must not fall back to the direct client")
	}
	_, err := client.Get(direct.URL)
	if err == nil {
		t.Fatal("expected strict invalid proxy client to fail closed")
	}
	if got := directHits.Load(); got != 0 {
		t.Fatalf("strict proxy failure leaked %d direct request(s)", got)
	}
}


func TestGetClientForConnection_StrictUnknownProxyTypeFailsClosed(t *testing.T) {
	var directHits atomic.Int32
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer direct.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}

	repo := db.NewRepo(database)
	pool, err := repo.InsertProxyPool(db.ProxyPoolData{
		Name:        "bad-type",
		ProxyURL:    "https://proxy.example.invalid",
		Type:        "mystery-proxy",
		StrictProxy: true,
	})
	if err != nil {
		t.Fatalf("failed to insert proxy pool: %v", err)
	}

	h := &ChatHandler{Client: &http.Client{}, Repo: repo}
	client := h.GetClientForConnection(&ConnectionData{ProxyPoolID: pool["id"].(string)})
	if client == h.Client {
		t.Fatal("strict unknown proxy type must not fall back to the direct client")
	}
	_, err = client.Get(direct.URL)
	if err == nil {
		t.Fatal("expected strict unknown proxy type to fail closed")
	}
	if got := directHits.Load(); got != 0 {
		t.Fatalf("strict unknown proxy type leaked %d direct request(s)", got)
	}
}


func TestGetClientForConnection_StrictMissingPoolFailsClosed(t *testing.T) {
	var directHits atomic.Int32
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer direct.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	h := &ChatHandler{Client: &http.Client{}, Repo: db.NewRepo(database)}

	client := h.GetClientForConnection(&ConnectionData{
		ProxyPoolID: "missing-pool",
		StrictProxy: true,
	})
	_, err := client.Get(direct.URL)
	if err == nil {
		t.Fatal("expected missing strict proxy pool to fail closed")
	}
	if got := directHits.Load(); got != 0 {
		t.Fatalf("missing strict proxy pool leaked %d direct request(s)", got)
	}
}

func TestGetClientForConnection_StrictPoolWithoutURLFailsClosed(t *testing.T) {
	var directHits atomic.Int32
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer direct.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create proxyPools: %v", err)
	}

	repo := db.NewRepo(database)
	pool, err := repo.InsertProxyPool(db.ProxyPoolData{
		Name:        "empty-strict",
		Type:        "http",
		StrictProxy: true,
	})
	if err != nil {
		t.Fatalf("insert empty strict pool: %v", err)
	}

	h := &ChatHandler{Client: &http.Client{}, Repo: repo}
	client := h.GetClientForConnection(&ConnectionData{ProxyPoolID: pool["id"].(string)})
	_, err = client.Get(direct.URL)
	if err == nil {
		t.Fatal("expected strict pool without URL to fail closed")
	}
	if got := directHits.Load(); got != 0 {
		t.Fatalf("strict pool without URL leaked %d direct request(s)", got)
	}
}

func TestGetClientForConnection_StrictInactivePoolFailsClosed(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create proxyPools: %v", err)
	}

	repo := db.NewRepo(database)
	pool, err := repo.InsertProxyPool(db.ProxyPoolData{
		Name:        "inactive-strict",
		ProxyURL:    "http://proxy.example.invalid:8080",
		Type:        "http",
		StrictProxy: true,
	})
	if err != nil {
		t.Fatalf("insert strict pool: %v", err)
	}
	poolID := pool["id"].(string)
	if _, err := database.Exec(`UPDATE proxyPools SET isActive = 0 WHERE id = ?`, poolID); err != nil {
		t.Fatalf("deactivate pool: %v", err)
	}

	h := &ChatHandler{Client: &http.Client{}, Repo: repo}
	client := h.GetClientForConnection(&ConnectionData{ProxyPoolID: poolID})
	if client == h.Client {
		t.Fatal("inactive strict pool must not return direct client")
	}
	_, err = client.Get("http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected inactive strict pool to fail closed")
	}
}
