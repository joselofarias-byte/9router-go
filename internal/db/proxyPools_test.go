package db

import (
	json "encoding/json/v2"
	"testing"
)

// TestInsertProxyPool_DataShape verifies InsertProxyPool writes the proxyPools
// row with the same data JSON shape and values the Next.js dashboard stores, so
// the shared DB stays compatible.
func TestInsertProxyPool_DataShape(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}

	repo := NewRepo(db)
	pool, err := repo.InsertProxyPool(ProxyPoolData{
		Name: "relay-x", ProxyURL: "https://relay-x.example.com", NoProxy: "", Type: "vercel", StrictProxy: false,
	})
	if err != nil {
		t.Fatalf("InsertProxyPool failed: %v", err)
	}
	if pool["id"].(string) == "" {
		t.Fatal("expected non-empty generated id")
	}
	if pool["isActive"] != true {
		t.Errorf("expected isActive=true, got %v", pool["isActive"])
	}

	var row struct {
		IsActive   int
		TestStatus string
		Data       string
		CreatedAt  string
		UpdatedAt  string
	}
	err = db.QueryRow(`SELECT isActive, testStatus, data, createdAt, updatedAt FROM proxyPools WHERE id = ?`, pool["id"]).
		Scan(&row.IsActive, &row.TestStatus, &row.Data, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		t.Fatalf("read back row: %v", err)
	}
	if row.IsActive != 1 {
		t.Errorf("expected isActive column 1, got %d", row.IsActive)
	}
	if row.TestStatus != "unknown" {
		t.Errorf("expected testStatus 'unknown', got %q", row.TestStatus)
	}
	if row.CreatedAt != row.UpdatedAt {
		t.Errorf("expected createdAt == updatedAt, got %q vs %q", row.CreatedAt, row.UpdatedAt)
	}

	// Parse the stored data JSON and assert it matches Next's createProxyPool shape.
	var data map[string]any
	if err := json.Unmarshal([]byte(row.Data), &data); err != nil {
		t.Fatalf("parse stored data: %v", err)
	}
	want := map[string]any{
		"name":         "relay-x",
		"proxyUrl":     "https://relay-x.example.com",
		"noProxy":      "",
		"type":         "vercel",
		"strictProxy":  false,
		"lastTestedAt": nil,
		"lastError":    nil,
	}
	if len(data) != len(want) {
		t.Fatalf("expected %d keys in data, got %d: %v", len(want), len(data), data)
	}
	for k, v := range want {
		if got, ok := data[k]; !ok || got != v {
			t.Errorf("data[%q] = %v (present=%v), want %v", k, got, ok, v)
		}
	}
}

func TestGetProxyPool_SingleProxyUrl_AndMetadata(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		data TEXT,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		createdAt TEXT,
		updatedAt TEXT
	);`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	repo := NewRepo(db)

	// Single proxyUrl string (Next.js format)
	poolData := `{"name":"relay-pool","proxyUrl":"https://relay.example.com","type":"vercel","noProxy":"localhost,*.internal","strictProxy":true}`
	if _, err := db.Exec(`INSERT INTO proxyPools (id, data, isActive) VALUES (?, ?, ?)`, "pool-single", poolData, 1); err != nil {
		t.Fatalf("insert: %v", err)
	}

	pool, err := repo.GetProxyPool("pool-single")
	if err != nil {
		t.Fatalf("GetProxyPool failed: %v", err)
	}
	if pool == nil {
		t.Fatal("expected pool, got nil")
	}
	if len(pool.URLs) != 1 || pool.URLs[0] != "https://relay.example.com" {
		t.Errorf("expected URLs [https://relay.example.com], got %v", pool.URLs)
	}
	if pool.Type != "vercel" {
		t.Errorf("expected Type vercel, got %s", pool.Type)
	}
	if pool.NoProxy != "localhost,*.internal" {
		t.Errorf("expected NoProxy localhost,*.internal, got %s", pool.NoProxy)
	}
	if !pool.StrictProxy {
		t.Errorf("expected StrictProxy true, got %v", pool.StrictProxy)
	}
	if next := pool.NextURL(); next != "https://relay.example.com" {
		t.Errorf("expected NextURL https://relay.example.com, got %s", next)
	}
}


func TestGetProxyPool_RefreshesChangedSharedDBConfig(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	proxyPoolCache.Delete("pool-refresh")
	defer proxyPoolCache.Delete("pool-refresh")

	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		data TEXT,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		createdAt TEXT,
		updatedAt TEXT
	);`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	repo := NewRepo(database)
	initial := `{"name":"shared","proxyUrl":"https://old.example","type":"http","strictProxy":false}`
	if _, err := database.Exec(`INSERT INTO proxyPools (id, data, isActive) VALUES (?, ?, 1)`, "pool-refresh", initial); err != nil {
		t.Fatalf("insert: %v", err)
	}

	first, err := repo.GetProxyPool("pool-refresh")
	if err != nil {
		t.Fatalf("first GetProxyPool: %v", err)
	}
	if first.StrictProxy || first.URLs[0] != "https://old.example" {
		t.Fatalf("unexpected initial pool: %#v", first)
	}

	updated := `{"name":"shared","proxyUrl":"https://new.example","type":"cloudflare","strictProxy":true}`
	if _, err := database.Exec(`UPDATE proxyPools SET data = ? WHERE id = ?`, updated, "pool-refresh"); err != nil {
		t.Fatalf("update shared DB: %v", err)
	}

	second, err := repo.GetProxyPool("pool-refresh")
	if err != nil {
		t.Fatalf("second GetProxyPool: %v", err)
	}
	if !second.StrictProxy || second.Type != "cloudflare" || len(second.URLs) != 1 || second.URLs[0] != "https://new.example" {
		t.Fatalf("expected refreshed pool config, got %#v", second)
	}
}

func TestProxyPoolNextURL_RoundRobinStartsWithFirstURL(t *testing.T) {
	pool := &ProxyPool{URLs: []string{"https://one.example", "https://two.example", "https://three.example"}}
	want := []string{"https://one.example", "https://two.example", "https://three.example", "https://one.example"}
	for i, expected := range want {
		if got := pool.NextURL(); got != expected {
			t.Fatalf("call %d: expected %s, got %s", i+1, expected, got)
		}
	}
}
