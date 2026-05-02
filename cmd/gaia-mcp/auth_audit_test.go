package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLogger returns a slog.Logger that writes JSON lines to a
// buffer the test can inspect. Mirrors the production wiring (JSON
// handler to stderr) so the assertions reflect what an operator
// actually sees.
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return logger, &buf
}

func parseLogLine(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parse log line: %v\nraw: %s", err, raw)
	}
	return m
}

func TestAuthAuditSuccess(t *testing.T) {
	tokens := tokenStore{"tok_alice": "alice@laptop"}
	logger, buf := captureLogger()
	handler := bearerAuthMiddleware(tokens, logger, labelEchoHandler())

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer tok_alice")
	req.RemoteAddr = "192.0.2.1:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", rec.Code)
	}

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected one audit line; got nothing")
	}
	rec1 := parseLogLine(t, line)
	if rec1["msg"] != "auth_success" {
		t.Errorf("msg: %+v", rec1)
	}
	if rec1["label"] != "alice@laptop" {
		t.Errorf("label: %+v", rec1)
	}
	if rec1["remote"] != "192.0.2.1:54321" {
		t.Errorf("remote: %+v", rec1)
	}
	if rec1["path"] != "/mcp" {
		t.Errorf("path: %+v", rec1)
	}
	// The token must NEVER appear in audit logs.
	if strings.Contains(line, "tok_alice") {
		t.Errorf("audit log leaks token: %s", line)
	}
}

func TestAuthAuditFailureReasons(t *testing.T) {
	tokens := tokenStore{"tok_alice": "alice"}

	cases := []struct {
		name       string
		auth       string
		wantReason string
	}{
		{"missing", "", "no_authorization_header"},
		{"basic", "Basic dXNlcjpwYXNz", "non_bearer_scheme"},
		{"empty", "Bearer ", "empty_bearer"},
		{"unknown", "Bearer tok_eve", "unknown_token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger, buf := captureLogger()
			handler := bearerAuthMiddleware(tokens, logger, labelEchoHandler())

			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			req.RemoteAddr = "203.0.113.7:55555"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want 401", rec.Code)
			}

			line := strings.TrimSpace(buf.String())
			rec1 := parseLogLine(t, line)
			if rec1["msg"] != "auth_failure" {
				t.Errorf("msg: %+v", rec1)
			}
			if rec1["reason"] != tc.wantReason {
				t.Errorf("reason: got %v, want %s", rec1["reason"], tc.wantReason)
			}
			if rec1["level"] != "WARN" {
				t.Errorf("level: %+v (failures should be WARN)", rec1["level"])
			}
			if strings.Contains(line, "tok_") {
				t.Errorf("audit log leaks token shape: %s", line)
			}
		})
	}
}

// TestClientAddrHonorsXFF confirms the audit-log "remote" field
// reflects the real client when X-Forwarded-For is set by an upstream
// proxy. Without this, a deployment behind nginx logs the proxy's IP
// for every request and the audit log loses its main forensic value.
func TestClientAddrHonorsXFF(t *testing.T) {
	cases := []struct {
		xff    string
		remote string
		want   string
	}{
		{"", "10.0.0.1:1234", "10.0.0.1:1234"},
		{"203.0.113.7", "10.0.0.1:1234", "203.0.113.7"},
		{"203.0.113.7, 10.0.0.99", "10.0.0.1:1234", "203.0.113.7"},
		{"  203.0.113.7  , 10.0.0.99", "10.0.0.1:1234", "203.0.113.7"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.RemoteAddr = tc.remote
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		got := clientAddr(req)
		if got != tc.want {
			t.Errorf("xff=%q remote=%q → got %q, want %q", tc.xff, tc.remote, got, tc.want)
		}
	}
}
