package db

import (
	"9router/proxy/internal/dbtest"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func fabricTestRepo(t *testing.T) *Repo {
	t.Helper()
	d, e := OpenDatabase(filepath.Join(t.TempDir(), "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	if e = dbtest.CreateTables(d); e != nil {
		t.Fatal(e)
	}
	return NewRepo(d)
}
func fabricFixture(t *testing.T, m string) FabricEnvelope {
	t.Helper()
	s := FabricSnapshot{1, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339), []FabricNode{{"fabric-test", "https://example.com/v1"}}, []FabricCombo{{"fabric-experimental", []string{"fabric-test/" + m}}}}
	b, e := json.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(b)
	return FabricEnvelope{hex.EncodeToString(h[:]), b}
}
func TestFabricTransactionRollbackAndCAS(t *testing.T) {
	r := fabricTestRepo(t)
	ctx := context.Background()
	a, b := fabricFixture(t, "one"), fabricFixture(t, "two")
	if _, e := r.ApplyFabric(ctx, &a, "", false); e != nil {
		t.Fatal(e)
	}
	if _, e := r.ApplyFabric(ctx, &b, "", false); !errors.Is(e, ErrFabricConflict) {
		t.Fatal(e)
	}
	if _, e := r.ApplyFabric(ctx, &b, a.Hash, false); e != nil {
		t.Fatal(e)
	}
	c, e := r.GetComboByName("fabric-experimental")
	if e != nil || c.Models != `["fabric-test/two"]` {
		t.Fatalf("%+v %v", c, e)
	}
	if _, e = r.ApplyFabric(ctx, nil, b.Hash, true); e != nil {
		t.Fatal(e)
	}
	s, e := r.FabricState(ctx)
	if e != nil || s.Current.Hash != a.Hash || s.Previous.Hash != b.Hash {
		t.Fatalf("%+v %v", s, e)
	}
}
func TestFabricInvalidSnapshots(t *testing.T) {
	for _, kind := range []string{"hash", "expiry", "production", "secret"} {
		t.Run(kind, func(t *testing.T) {
			e := fabricFixture(t, "one")
			var s map[string]any
			json.Unmarshal(e.Payload, &s)
			switch kind {
			case "hash":
				e.Hash = "bad"
			case "expiry":
				s["issuedAt"] = "2020-01-01T00:00:00Z"
				s["expiresAt"] = "2020-01-02T00:00:00Z"
			case "production":
				s["combos"] = []any{map[string]any{"name": "production", "models": []string{"fabric-test/one"}}}
			case "secret":
				s["apiKey"] = "must-not-pass"
			}
			if kind != "hash" {
				e.Payload, _ = json.Marshal(s)
				h := sha256.Sum256(e.Payload)
				e.Hash = hex.EncodeToString(h[:])
			}
			if _, err := ValidateFabric(e, time.Now(), false); err == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
}
func TestFabricAtomicOwnershipConflict(t *testing.T) {
	r := fabricTestRepo(t)
	e := fabricFixture(t, "one")
	_, err := r.db.Exec(`INSERT INTO combos(id,name,models,createdAt,updatedAt) VALUES('unmanaged','fabric-experimental','[]','','')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyFabric(context.Background(), &e, "", false); err == nil {
		t.Fatal("conflict accepted")
	}
	var n int
	r.db.QueryRow(`SELECT count(*) FROM providerNodes`).Scan(&n)
	if n != 0 {
		t.Fatal("partial write")
	}
	s, _ := r.FabricState(context.Background())
	if s.Current != nil {
		t.Fatal("failed transaction installed state")
	}
}
func TestFabricExpiryKeepsRoutes(t *testing.T) {
	r := fabricTestRepo(t)
	e := fabricFixture(t, "one")
	if _, err := r.ApplyFabric(context.Background(), &e, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateFabric(e, time.Now().Add(2*time.Hour), false); err == nil {
		t.Fatal("expired accepted")
	}
	c, _ := r.GetComboByName("fabric-experimental")
	if c == nil {
		t.Fatal("last good lost")
	}
}
func TestFabricRejectsManagementDrift(t *testing.T) {
	r := fabricTestRepo(t)
	a, b := fabricFixture(t, "one"), fabricFixture(t, "two")
	ctx := context.Background()
	if _, e := r.ApplyFabric(ctx, &a, "", false); e != nil {
		t.Fatal(e)
	}
	r.db.Exec(`UPDATE combos SET models='["manual/model"]' WHERE name='fabric-experimental'`)
	if _, e := r.ApplyFabric(ctx, &b, a.Hash, false); !errors.Is(e, ErrFabricConflict) {
		t.Fatalf("management change overwritten: %v", e)
	}
}
