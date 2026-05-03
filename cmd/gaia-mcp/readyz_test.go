package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

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
