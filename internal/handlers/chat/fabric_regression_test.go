package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFabricEligibilityRotation(t *testing.T) {
	h, close := setupHandlerForForward(t)
	defer close()
	d := h.Repo.RawDB()
	for _, id := range []string{"a", "b", "c"} {
		seedConnDB(t, d, "fabric-test", id, "synthetic-key", "http://127.0.0.1:1")
	}
	d.Exec(`UPDATE providerConnections SET priority=2 WHERE id='c'`)
	d.Exec(`INSERT INTO settings(id,data) VALUES(1,'{"providerStrategies":{"fabric-test":{"rotateStrategy":"round-robin"}}}')`)
	for _, want := range []string{"a", "b", "a", "b"} {
		c, _, err := h.getBestConnection("fabric-test", "", nil, "m")
		if err != nil || c.ID != want {
			t.Fatalf("%+v %v want %s", c, err, want)
		}
	}
	h.Repo.LockConnectionModel("a", "m", 60, 1)
	if _, _, err := h.getBestConnection("fabric-test", "a", nil, "m"); err == nil {
		t.Fatal("locked pinned accepted")
	}
	d.Exec(`UPDATE providerConnections SET isActive=0 WHERE id='b'`)
	if _, _, err := h.getBestConnection("fabric-test", "b", nil, "m"); err == nil {
		t.Fatal("inactive accepted")
	}
	if _, _, err := h.getBestConnection("other", "c", nil, "m"); err == nil {
		t.Fatal("provider mismatch accepted")
	}
	c, _, err := h.getBestConnection("fabric-test", "", nil, "m")
	if err != nil || c.ID != "c" {
		t.Fatalf("%+v %v", c, err)
	}
}
func TestFabricFallbackPrivacyAndLocks(t *testing.T) {
	for _, locked := range []bool{true, false} {
		t.Run(map[bool]string{true: "locked", false: "rate-limit"}[locked], func(t *testing.T) {
			h, close := setupHandlerForForward(t)
			defer close()
			var calls atomic.Int32
			bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(429)
				w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			}))
			defer bad.Close()
			good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"model":"test","choices":[{"message":{"role":"assistant","content":"PRIVATE_RESPONSE_SENTINEL"}}],"usage":{"prompt_tokens":2,"completion_tokens":2}}`))
			}))
			defer good.Close()
			seedConnDB(t, h.Repo.RawDB(), "fabric-test", "a", "synthetic-a", bad.URL)
			seedConnDB(t, h.Repo.RawDB(), "fabric-test", "b", "synthetic-b", good.URL)
			h.Repo.RawDB().Exec(`UPDATE providerConnections SET priority=2 WHERE id='b'`)
			if locked {
				h.Repo.LockConnectionModel("a", "test", 60, 1)
			}
			w := newCommittedResponseWriter(httptest.NewRecorder())
			err := h.handleAccountFallback(context.Background(), w, "fabric-test", "test", "", []byte(`{"model":"test","messages":[{"role":"user","content":"PRIVATE_PROMPT_SENTINEL"}]}`), false, false, "test")
			if err != nil {
				t.Fatal(err)
			}
			want := int32(1)
			if locked {
				want = 0
			}
			if calls.Load() != want {
				t.Fatal("wrong account attempts")
			}
			isLocked, e := h.Repo.IsConnectionModelLocked("a", "test")
			if e != nil || !isLocked {
				t.Fatal("lock missing")
			}
			if !locked {
				var until string
				if e = h.Repo.RawDB().QueryRow(`SELECT json_extract(data,'$.modelLock_test') FROM providerConnections WHERE id='a'`).Scan(&until); e != nil {
					t.Fatal(e)
				}
				deadline, e := time.Parse(time.RFC3339, until)
				if e != nil || time.Until(deadline) < 110*time.Second {
					t.Fatal("Retry-After header lost")
				}
			}

			var raw string
			if e = h.Repo.RawDB().QueryRow(`SELECT data FROM requestDetails LIMIT 1`).Scan(&raw); e != nil {
				t.Fatal(e)
			}
			if strings.Contains(raw, "PRIVATE_") {
				t.Fatal("conversation persisted")
			}
		})
	}
}
func TestFabricFlushCommitted(t *testing.T) {
	w := newCommittedResponseWriter(httptest.NewRecorder())
	w.Flush()
	if !w.IsCommitted() {
		t.Fatal("unsafe retry")
	}
}

func TestFabricRetryAfter(t *testing.T) {
	e := &upstreamError{StatusCode: 429, RetryAfter: "120"}
	if accountRetryDelay(e, time.Now()) != 120 {
		t.Fatal("retry after ignored")
	}
	e.RetryAfter = ""
	e.Body = []byte(`{"resetsAt":"2099-01-01T00:00:00Z"}`)
	if accountRetryDelay(e, time.Now()) <= 0 {
		t.Fatal("quota reset ignored")
	}
}

func TestFabricPinnedProbeHonorsQuota(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			h, close := setupHandlerForForward(t)
			defer close()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(status)
				w.Write([]byte(`{"error":{"message":"synthetic failure"}}`))
			}))
			defer server.Close()
			seedConnDB(t, h.Repo.RawDB(), "fabric-test", "pin", "synthetic", server.URL)
			for i := 0; i < 2; i++ {
				if err := h.handleAccountFallback(context.Background(), newCommittedResponseWriter(httptest.NewRecorder()), "fabric-test", "m", "pin", []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`), false, false, "test"); err == nil {
					t.Fatal("failed account accepted")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("pinned account ignored cooldown")
			}
		})
	}
}
