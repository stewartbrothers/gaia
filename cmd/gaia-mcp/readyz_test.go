package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

// fakeBuilderError builds a provider builder that always fails — the
// "no provider configured" path readyz hits when the operator hasn't
// run `gaia auth forgejo` yet.
func fakeBuilderError(err error) func(context.Context) (provider.Provider, error) {
	return func(_ context.Context) (provider.Provider, error) { return nil, err }
}

// fakeForgeProviderForReadyz returns a *forgejo.Provider hitting the
// supplied test server, packaged in a builder closure for readyzHandler.
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

func TestReadyzReady(t *testing.T) {
	build := fakeForgeProviderForReadyz(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("expected Whoami → /user; got %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"login":"alice"}`))
	})

	rec := httptest.NewRecorder()
	readyzHandler(build, discardLogger(), 2*time.Second).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "ready" {
		t.Errorf("body: %q", body)
	}
}

func TestReadyzUnreadyOnBuildFailure(t *testing.T) {
	build := fakeBuilderError(errors.New("no provider configured"))

	rec := httptest.NewRecorder()
	readyzHandler(build, discardLogger(), 2*time.Second).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", rec.Code)
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "unready" {
		t.Errorf("body: %q", body)
	}
}

func TestReadyzUnreadyOnForgePingFailure(t *testing.T) {
	// Server returns 401: a token that exists but is invalid. Same
	// readiness signal as a network error — the daemon can't do
	// useful work, take it out of rotation.
	build := fakeForgeProviderForReadyz(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	rec := httptest.NewRecorder()
	readyzHandler(build, discardLogger(), 2*time.Second).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", rec.Code)
	}
}

// TestReadyzUnreadyDoesntLeakError pins the opaque-body invariant —
// the response body must not echo upstream error detail. Detail goes
// to the logger; the wire is opaque so /readyz isn't a probe vector.
func TestReadyzUnreadyDoesntLeakError(t *testing.T) {
	build := fakeBuilderError(errors.New("config: secret-database-url-postgres://creds@host/db"))

	rec := httptest.NewRecorder()
	readyzHandler(build, discardLogger(), 2*time.Second).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	body, _ := io.ReadAll(rec.Body)
	if string(body) != "unready" {
		t.Errorf("body must be opaque 'unready'; got %q", body)
	}
}

// TestReadyzHonorsTimeout verifies the per-request timeout fires when
// the upstream is hanging. Without the timeout, a stuck forge would
// hold the readyz handler forever and the orchestrator's probe
// timeout (typically 1-3s) would be the only thing protecting us.
func TestReadyzHonorsTimeout(t *testing.T) {
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
	readyzHandler(build, discardLogger(), 100*time.Millisecond).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(context.Background()))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503 (timeout)", rec.Code)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Errorf("readyz didn't honor timeout; took %v", elapsed)
	}
}
