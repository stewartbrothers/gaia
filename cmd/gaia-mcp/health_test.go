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
// bearer token, even when token-file auth is configured for the MCP
// path. This is the contract orchestrators rely on: the health probe
// has no credentials.
func TestHealthzMountedAndUnauthenticated(t *testing.T) {
	addr := "127.0.0.1:" + freePort(t)
	cfg := httpConfig{
		Addr:              addr,
		BasePath:          "/mcp",
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ShutdownTimeout:   500 * time.Millisecond,
	}
	tokens := tokenStore{"tok_x": "alice"}

	done := make(chan error, 1)
	go func() { done <- runHTTP(cfg, buildServer(), tokens) }()
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

	// MCP path: no auth → 401 (token-file is configured)
	resp2, err := http.Get("http://" + addr + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("/mcp unauthenticated: got %d, want 401", resp2.StatusCode)
	}
}
