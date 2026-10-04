package freecoding

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"9router/proxy/internal/controlplane/discovery"
)

const (
	fixtureOpenRouterProvenance = "fixture:testdata/catalogs/openrouter.json classified by harness OpenRouter catalog rules (:free and zero price; no Authorization)"
	fixtureClineProvenance      = "fixture:testdata/catalogs/cline.json classified by discovery.ClineFreeAdapter"
)

// captureTransport records the last HTTP status and refuses credentials on
// catalog reads. Public discovery must not send Authorization.
type captureTransport struct {
	base   http.RoundTripper
	status int
}

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
		return nil, fmt.Errorf("public discovery must not send credentials")
	}
	base := c.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(r)
	if resp != nil {
		c.status = resp.StatusCode
	}
	return resp, err
}

func wrapClient(base *http.Client) (*http.Client, *captureTransport) {
	if base == nil {
		base = &http.Client{Timeout: 30 * time.Second}
	}
	tr := base.Transport
	if tr == nil {
		tr = http.DefaultTransport
	}
	cap := &captureTransport{base: tr}
	clone := *base
	clone.Transport = cap
	if clone.Timeout == 0 {
		clone.Timeout = 30 * time.Second
	}
	return &clone, cap
}

var clineURLMu sync.Mutex

func observeOpenRouter(ctx context.Context, client *http.Client, catalogURL string) ([]discovery.Candidate, int, time.Duration, error) {
	wrapped, cap := wrapClient(client)
	start := time.Now()
	got, err := discoverOpenRouterCatalog(ctx, wrapped, catalogURL)
	return got, cap.status, time.Since(start), err
}

func observeCline(ctx context.Context, client *http.Client, catalogURL string) ([]discovery.Candidate, int, time.Duration, error) {
	wrapped, cap := wrapClient(client)
	start := time.Now()
	var got []discovery.Candidate
	var err error
	if catalogURL == "" {
		got, err = discovery.NewClineFreeAdapter(wrapped).Discover(ctx)
		return got, cap.status, time.Since(start), err
	}
	clineURLMu.Lock()
	defer clineURLMu.Unlock()
	old := discovery.ClineRecommendedModelsURL
	discovery.ClineRecommendedModelsURL = catalogURL
	defer func() { discovery.ClineRecommendedModelsURL = old }()
	got, err = discovery.NewClineFreeAdapter(wrapped).Discover(ctx)
	return got, cap.status, time.Since(start), err
}

func catalogObservation(provider, tier, provenance string, now time.Time, status int, lat time.Duration, cands []discovery.Candidate, trials []Record, callErr error) Record {
	rec := Record{
		RecordType:     RecordCatalog,
		UTC:            now.UTC().Format(time.RFC3339),
		Tier:           tier,
		Provider:       provider,
		HTTPStatus:     status,
		LatencyMs:      lat.Milliseconds(),
		Provenance:     provenance,
		QuotaScope:     QuotaNotApplicable,
		CandidateCount: len(cands),
		Note:           fmt.Sprintf("free_or_free_tier=%d", len(trials)),
	}
	if callErr != nil {
		rec.Skip = true
		rec.SkipReason = "catalog request failed"
		rec.Error = truncate(callErr.Error(), 500)
		rec.Note = "catalog request failed; previous rows were not replaced by this harness"
	}
	return rec
}

// DiscoverFixtures classifies the embedded catalog fixtures. Cline goes
// through discovery.ClineFreeAdapter. OpenRouter uses the harness catalog
// rules. No public network call is made.
func DiscoverFixtures(ctx context.Context, now time.Time) ([]Record, error) {
	openBody, err := fixtureFS.ReadFile("testdata/catalogs/openrouter.json")
	if err != nil {
		return nil, err
	}
	clineBody, err := fixtureFS.ReadFile("testdata/catalogs/cline.json")
	if err != nil {
		return nil, err
	}
	var out []Record
	openRecs, err := classifyFixture(ctx, "openrouter", openBody, fixtureOpenRouterProvenance, now, func(ctx context.Context, srv *httptest.Server) ([]discovery.Candidate, int, time.Duration, error) {
		return observeOpenRouter(ctx, srv.Client(), srv.URL)
	})
	if err != nil {
		return nil, err
	}
	clineRecs, err := classifyFixture(ctx, "cline", clineBody, fixtureClineProvenance, now, func(ctx context.Context, srv *httptest.Server) ([]discovery.Candidate, int, time.Duration, error) {
		return observeCline(ctx, srv.Client(), srv.URL)
	})
	if err != nil {
		return nil, err
	}
	out = append(out, clineRecs...)
	out = append(out, openRecs...)
	return out, nil
}

type fixtureObserve func(context.Context, *httptest.Server) ([]discovery.Candidate, int, time.Duration, error)

func classifyFixture(ctx context.Context, provider string, body []byte, provenance string, now time.Time, obs fixtureObserve) ([]Record, error) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "credentials refused", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	cands, status, lat, err := obs(ctx, srv)
	if err != nil {
		return nil, fmt.Errorf("%s fixture: %w", provider, err)
	}
	trials := CollectTrials(cands, TierOffline, provenance, now)
	rec := catalogObservation(provider, TierOffline, provenance, now, status, lat, cands, trials, nil)
	out := make([]Record, 0, 1+len(trials))
	out = append(out, rec)
	out = append(out, trials...)
	return out, nil
}

// DiscoverPublic reads the official OpenRouter and Cline catalogs without
// credentials. A failure of one source is recorded and does not drop the
// other. Trial rows are eligibility for a later authenticated run, not a
// coding ranking.
func DiscoverPublic(ctx context.Context, now time.Time) ([]Record, error) {
	var out []Record
	for _, src := range []struct {
		provider   string
		provenance string
		fn         func(context.Context) ([]discovery.Candidate, int, time.Duration, error)
	}{
		{"cline", discovery.ClineRecommendedModelsURL, func(ctx context.Context) ([]discovery.Candidate, int, time.Duration, error) {
			return observeCline(ctx, nil, "")
		}},
		{"openrouter", openRouterModelsURL, func(ctx context.Context) ([]discovery.Candidate, int, time.Duration, error) {
			return observeOpenRouter(ctx, nil, "")
		}},
	} {
		cands, status, lat, err := src.fn(ctx)
		var trials []Record
		if err == nil {
			trials = CollectTrials(cands, TierDiscovery, src.provenance, now)
		}
		out = append(out, catalogObservation(src.provider, TierDiscovery, src.provenance, now, status, lat, cands, trials, err))
		out = append(out, trials...)
	}
	return out, nil
}

// CatalogsFailed reports whether every catalog observation failed and no
// trial candidate was recorded.
func CatalogsFailed(recs []Record) bool {
	saw := false
	for _, rec := range recs {
		switch rec.RecordType {
		case RecordCatalog:
			saw = true
			if !rec.Skip {
				return false
			}
		case RecordTrial:
			return false
		}
	}
	return saw
}
