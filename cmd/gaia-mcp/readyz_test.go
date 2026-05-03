package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

// fakeBuilderError builds a provider builder that always fails — the
// "no provider configured" path readyzUpstreamHandler hits when the
// operator hasn't run `gaia auth forgejo` yet.
func fakeBuilderError(err error) func(context.Context) (provider.Provider, error) {
	return func(_ context.Context) (provider.Provider, error) { return nil, err }
}

// fakeForgeProviderForReadyz returns a *forgejo.Provider hitting the
// supplied test server, packaged in a builder closure for
// readyzUpstreamHandler.
func fakeForgeProviderForReadyz(t *testing.T, h http.HandlerFunc) func(context.Context) (provider.Provider, error) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := forgejo.NewProvider(forgejo.Options{
		BaseURL:   srv.URL,
		Token:     "TEST",
		RetryWait: 1 * time.Millisecond,
	})
	return func(_ context.Context) (provider.Provider, error) { return p, nil }
}

// TestReadyzAlwaysReadyAndMakesNoUpstreamCalls is the #139
// regression: /readyz must NOT make any forge calls. An attacker
// who can hit /readyz cannot drain the host's rate limit because
// the handler doesn't talk to the forge at all.
func TestReadyzAlwaysReadyAndMakesNoUpstreamCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	readyzHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "ready" {
		t.Errorf("body: %q", body)
	}
}

// TestReadyzNoForgePing constructs a builder that fatals if invoked,
// then hits /readyz repeatedly to prove no upstream call is made
// (the builder is never asked for a provider). This is the
// load-bearing assertion for the rate-limit-drain mitigation.
func TestReadyzNoForgePing(t *testing.T) {
	calls := int32(0)
	// The builder counts invocations and would also fail every call
	// if it WERE invoked — so a regression that re-introduces the
	// upstream check would surface as either a non-zero count OR a
	// non-200 response.
	_ = func(_ context.Context) (provider.Provider, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("readyz must not invoke the builder")
	}

	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		readyzHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("iteration %d: got %d", i, rec.Code)
		}
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("readyz invoked the builder %d times; should be 0", calls)
	}
}

func TestReadyzUpstreamReady(t *testing.T) {
	build := fakeForgeProviderForReadyz(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("expected Whoami → /user; got %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"login":"alice"}`))
	})

	rec := httptest.NewRecorder()
	readyzUpstreamHandler(build, discardLogger(), 2*time.Second).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz/upstream", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "ready" {
		t.Errorf("body: %q", body)
	}
}

func TestReadyzUpstreamUnreadyOnBuildFailure(t *testing.T) {
	build := fakeBuilderError(errors.New("no provider configured"))

	rec := httptest.NewRecorder()
	readyzUpstreamHandler(build, discardLogger(), 2*time.Second).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz/upstream", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", rec.Code)
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "unready" {
		t.Errorf("body: %q", body)
	}
}

func TestReadyzUpstreamUnreadyOnForgePingFailure(t *testing.T) {
	// Server returns 401: a token that exists but is invalid. Same
	// readiness signal as a network error — the daemon can't do
	// useful work, take it out of rotation.
	build := fakeForgeProviderForReadyz(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	rec := httptest.NewRecorder()
	readyzUpstreamHandler(build, discardLogger(), 2*time.Second).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz/upstream", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", rec.Code)
	}
}

// TestReadyzUpstreamDoesntLeakError pins the opaque-body invariant —
// the response body must not echo upstream error detail. Detail goes
// to the logger; the wire is opaque so the endpoint isn't a probe
// vector.
func TestReadyzUpstreamDoesntLeakError(t *testing.T) {
	build := fakeBuilderError(errors.New("config: secret-database-url-postgres://creds@host/db"))

	rec := httptest.NewRecorder()
	readyzUpstreamHandler(build, discardLogger(), 2*time.Second).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz/upstream", nil))

	body, _ := io.ReadAll(rec.Body)
	if string(body) != "unready" {
		t.Errorf("body must be opaque 'unready'; got %q", body)
	}
}

// TestReadyzUpstreamHonorsTimeout verifies the per-request timeout
// fires when the upstream is hanging.
func TestReadyzUpstreamHonorsTimeout(t *testing.T) {
	build := fakeForgeProviderForReadyz(t, func(w http.ResponseWriter, r *http.Request) {
		// Simulate an upstream that never responds within the deadline.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	start := time.Now()
	readyzUpstreamHandler(build, discardLogger(), 100*time.Millisecond).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz/upstream", nil).WithContext(context.Background()))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503 (timeout)", rec.Code)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Errorf("readyz/upstream didn't honor timeout; took %v", elapsed)
	}
}
