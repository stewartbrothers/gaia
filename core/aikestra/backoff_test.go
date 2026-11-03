package aikestra

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

// shedThenOK answers the first request with status/code and Retry-After
// ra, then succeeds with a comment body.
func shedThenOK(t *testing.T, status int, code, ra string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			if ra != "" {
				w.Header().Set("Retry-After", ra)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"m"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"c1","body":"x","author":"ada@example.com","created_at":"2026-11-03T00:00:00Z","updated_at":"2026-11-03T00:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// recordWaits replaces the client's sleep so tests see the honoured
// Retry-After without waiting for it.
func recordWaits(p *Provider) *[]time.Duration {
	var waits []time.Duration
	p.client.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	return &waits
}

// Briefing v1: 503 busy and 429 rate_limited carry Retry-After; honour
// it. The server shed the request before handling it, so even a write
// is retried — once.
func TestShedLoad_HonoursRetryAfterOnWrites(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{http.StatusServiceUnavailable, "busy"}, {http.StatusTooManyRequests, "rate_limited"}} {
		t.Run(tc.code, func(t *testing.T) {
			var calls atomic.Int32
			srv := shedThenOK(t, tc.status, tc.code, "2", &calls)
			p := NewProvider(Options{BaseURL: srv.URL})
			waits := recordWaits(p)
			if _, err := p.CreateIssueComment(context.Background(), "acme", "", 1, "x"); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 || len(*waits) != 1 || (*waits)[0] != 2*time.Second {
				t.Fatalf("calls %d waits %v", calls.Load(), *waits)
			}
		})
	}
}

func TestShedLoad_CapsRetryAfterAndDefaultsWithoutIt(t *testing.T) {
	var calls atomic.Int32
	p := NewProvider(Options{BaseURL: shedThenOK(t, http.StatusServiceUnavailable, "busy", "3600", &calls).URL})
	waits := recordWaits(p)
	if _, err := p.Whoami(context.Background()); err != nil {
		t.Fatal(err)
	}
	if (*waits)[0] != maxRetryAfter {
		t.Errorf("wait %v, want cap %v", (*waits)[0], maxRetryAfter)
	}

	calls.Store(0)
	p = NewProvider(Options{BaseURL: shedThenOK(t, http.StatusTooManyRequests, "rate_limited", "", &calls).URL, RetryWait: 7 * time.Millisecond})
	waits = recordWaits(p)
	if _, err := p.Whoami(context.Background()); err != nil {
		t.Fatal(err)
	}
	if (*waits)[0] != 7*time.Millisecond {
		t.Errorf("wait %v, want RetryWait", (*waits)[0])
	}
}

// Still shedding after the one retry: give up with the code agents
// branch on — RateLimit for 429, Network (transient) for busy.
func TestShedLoad_GivesUpAfterOneRetry(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		exit   int
	}{{http.StatusServiceUnavailable, "busy", exitcode.Network}, {http.StatusTooManyRequests, "rate_limited", exitcode.RateLimit}} {
		t.Run(tc.code, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"code":"` + tc.code + `","message":"m"}}`))
			}))
			defer srv.Close()
			p := NewProvider(Options{BaseURL: srv.URL})
			recordWaits(p)
			_, err := p.CreateIssueComment(context.Background(), "acme", "", 1, "x")
			wantCode(t, err, tc.exit, tc.code)
			if calls.Load() != 2 {
				t.Fatalf("calls %d", calls.Load())
			}
		})
	}
}
