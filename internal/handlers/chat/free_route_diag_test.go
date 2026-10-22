package chat

import (
	"strings"
	"testing"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/db"
)

func TestExplainFreeRouteSeparatesAccountStates(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'groq'`); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	seeds := []struct {
		id     string
		status int
		active int
	}{
		{"acct-auth", 401, 1},
		{"acct-quota", 402, 1},
		{"acct-rate", 429, 1},
		{"acct-gw", 503, 1},
		{"acct-ok", 0, 1},
		{"acct-off", 401, 0},
	}
	for _, seed := range seeds {
		data := `{"apiKey":"gsk-` + seed.id + `-secret"}`
		if seed.status != 0 {
			data = `{"apiKey":"gsk-` + seed.id + `-secret","rateLimitedUntil":"` + until + `","lastError":{"status":` + itoa(seed.status) + `,"message":"Bearer gsk-` + seed.id + `-secret"}}`
		}
		if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, email, priority, isActive, data, createdAt, updatedAt) VALUES (?, 'groq', 'apikey', 'Groq', 'hidden@example.com', 1, ?, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
			seed.id, seed.active, data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`UPDATE providerConnections SET email = 'hidden-deepseek@example.com' WHERE id = 'conn-1'`); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO requestDetails (id, timestamp, provider, model, connectionId, status, data) VALUES (?, ?, 'groq', 'llama-3.1-8b-instant', 'acct-ok', 'success', '{"latency":{"total":40},"response":{"status":200}}')`, "rd-diag", stamp); err != nil {
		t.Fatal(err)
	}
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))
	diag := h.ExplainFreeRoute("free-best")
	raw := mustJSON(t, diag)
	for _, banned := range []string{"gsk-", "secret", "Bearer", "apiKey", "hidden@example.com", "hidden-deepseek", `"quota":0`, `"quota":"0"`, `"quota":"available"`, `"remaining"`} {
		if strings.Contains(raw, banned) {
			t.Fatalf("diagnosis contained %q: %s", banned, raw)
		}
	}

	var groq *FreeRouteHop
	for i := range diag.Selected {
		if diag.Selected[i].Provider == "groq" {
			groq = &diag.Selected[i]
		}
		if diag.Selected[i].Provider == "deepseek" && diag.Selected[i].Pricing != "free_tier" {
			t.Fatalf("deepseek pricing = %q, want free_tier", diag.Selected[i].Pricing)
		}
	}
	if groq == nil {
		t.Fatalf("groq hop missing: %#v", diag.Selected)
	}
	if groq.Pricing != "free" {
		t.Fatalf("groq pricing = %q, want the registry mode free", groq.Pricing)
	}
	if groq.Availability != availabilityRecentSuccess {
		t.Fatalf("availability = %q, want recent_success", groq.Availability)
	}
	want := map[string]FreeRouteAccount{
		"acct-auth":  {AccountID: "acct-auth", State: accountStateAuthFailure, Quota: quotaUnknown, QuotaKnown: false},
		"acct-quota": {AccountID: "acct-quota", State: accountStateQuotaExhausted, Quota: quotaExhausted, QuotaKnown: true},
		"acct-rate":  {AccountID: "acct-rate", State: accountStateRateLimited, Quota: quotaRateLimited, QuotaKnown: true},
		"acct-gw":    {AccountID: "acct-gw", State: accountStateGateway, Quota: quotaUnknown, QuotaKnown: false},
		"acct-ok":    {AccountID: "acct-ok", State: accountStateEligible, Quota: quotaUnknown, QuotaKnown: false},
		"acct-off":   {AccountID: "acct-off", State: accountStateInactive, Quota: quotaUnknown, QuotaKnown: false},
	}
	if len(groq.Accounts) != len(want) {
		t.Fatalf("groq accounts = %#v", groq.Accounts)
	}
	for _, got := range groq.Accounts {
		exp, ok := want[got.AccountID]
		if !ok || got != exp {
			t.Fatalf("account %s = %#v, want %#v", got.AccountID, got, exp)
		}
		if got.QuotaKnown && (got.Quota == quotaUnknown || got.Quota == "0" || got.Quota == "available") {
			t.Fatalf("known quota must name the condition, got %#v", got)
		}
		if !got.QuotaKnown && got.Quota != quotaUnknown {
			t.Fatalf("unknown quota presented as %q on %#v", got.Quota, got)
		}
	}
}

func TestExplainFreeRouteShowsModelLockWithoutInventingQuota(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'groq'`); err != nil {
		t.Fatal(err)
	}
	seedConnDB(t, database, "groq", "acct-groq", "gsk-should-not-leak", "http://127.0.0.1:9")
	repo := db.NewRepo(database)
	if err := repo.LockConnectionModel("acct-groq", "llama-3.1-8b-instant", 300, 0); err != nil {
		t.Fatal(err)
	}
	seedFreeRouteRegistry(t)
	state := registry.GetActiveState()
	state.ProviderModels["groq"]["llama-3.3-70b-versatile"] = &registry.ProviderModel{
		ProviderID:    "groq",
		ModelID:       "llama-3.3-70b-versatile",
		UpstreamModel: "llama-3.3-70b-versatile",
		PricingMode:   "free",
		IsActive:      true,
	}
	h := NewChatHandler(repo)
	diag := h.ExplainFreeRoute("free-best")
	raw := mustJSON(t, diag)
	for _, banned := range []string{"gsk-", `"quota":0`, `"quota":"0"`, `"quota":"available"`, `"remaining"`} {
		if strings.Contains(raw, banned) {
			t.Fatalf("diagnosis contained %q: %s", banned, raw)
		}
	}
	locked := accountOnHop(t, diag, "llama-3.1-8b-instant", "acct-groq")
	open := accountOnHop(t, diag, "llama-3.3-70b-versatile", "acct-groq")
	if locked.State != accountStateModelLocked || locked.Quota != quotaUnknown || locked.QuotaKnown {
		t.Fatalf("locked model shown as %#v", locked)
	}
	if open.State != accountStateEligible || open.Quota != quotaUnknown || open.QuotaKnown {
		t.Fatalf("open model shown as %#v", open)
	}
}

func accountOnHop(t *testing.T, diag FreeRouteDiagnosis, model, accountID string) FreeRouteAccount {
	t.Helper()
	for _, hop := range diag.Selected {
		if hop.Model != model {
			continue
		}
		return accountByID(t, hop.Accounts, accountID)
	}
	t.Fatalf("hop %s missing from %#v", model, diag.Selected)
	return FreeRouteAccount{}
}

func TestClassifyAccountDoesNotTreatUnknownCooldownAsExhausted(t *testing.T) {
	until := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	got := classifyAccount("acct", true, `{"rateLimitedUntil":"`+until+`","lastError":{"status":500,"message":"boom"}}`)
	if got.State != accountStateCooldown || got.Quota != quotaUnknown || got.QuotaKnown {
		t.Fatalf("unclassified cooldown = %#v", got)
	}
	idle := classifyAccount("idle", true, `{"apiKey":"sk-local"}`)
	if idle.Quota != quotaUnknown || idle.QuotaKnown || idle.State != accountStateEligible {
		t.Fatalf("idle account = %#v", idle)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [4]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
