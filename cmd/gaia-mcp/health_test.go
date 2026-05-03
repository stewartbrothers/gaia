package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHealthzReturns200Ok(t *testing.T) {
	rec := httptest.NewRecorder()
	healthzHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "ok" {
		t.Errorf("body: got %q, want %q", body, "ok")
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("content-type: got %q", got)
	}
}

// TestHealthzMountedAndUnauthenticated runs the production runHTTP
// path end-to-end and confirms /healthz responds 200 *without* a
// bearer token, while /mcp without one still 401s. This is the
// contract orchestrators rely on: the health probe has no
// credentials.
func TestHealthzMountedAndUnauthenticated(t *testing.T) {
	addr := "127.0.0.1:" + freePort(t)
	cfg := httpConfig{
		Addr:              addr,
		BasePath:          "/mcp",
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ShutdownTimeout:   500 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() { done <- runHTTP(cfg, buildServer()) }()
	t.Cleanup(func() {
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		<-done
	})
	if err := waitListen(t, addr, 2*time.Second); err != nil {
		t.Fatalf("server never came up: %v", err)
	}

	// /healthz: no auth → 200
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz unauthenticated: got %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("/healthz body: %q", body)
	}

	// MCP path: no auth → 401 (pass-through middleware refuses requests
	// without a Bearer; the bearer would have been the user's forge PAT).
	resp2, err := http.Get("http://" + addr + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("/mcp unauthenticated: got %d, want 401", resp2.StatusCode)
	}

	// /readyz: no auth → 200 (#139). Liveness-only — no upstream
	// call, no rate-limit consumption.
	resp3, err := http.Get("http://" + addr + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("/readyz unauthenticated: got %d, want 200", resp3.StatusCode)
	}
	body3, _ := io.ReadAll(resp3.Body)
	if string(body3) != "ready" {
		t.Errorf("/readyz body: %q", body3)
	}

	// /readyz/upstream: no auth → 401 (#139). Bearer required so
	// each caller spends only their own forge quota on the
	// upstream Whoami probe.
	resp4, err := http.Get("http://" + addr + "/readyz/upstream")
	if err != nil {
		t.Fatalf("GET /readyz/upstream: %v", err)
	}
	defer func() { _ = resp4.Body.Close() }()
	if resp4.StatusCode != http.StatusUnauthorized {
		t.Errorf("/readyz/upstream unauthenticated: got %d, want 401", resp4.StatusCode)
	}
}
