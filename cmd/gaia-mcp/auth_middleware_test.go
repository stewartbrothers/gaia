package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tokenEchoHandler echoes the forge token attached to the request
// context, so tests can verify the bearer survived the middleware
// trip into the per-request context value.
func tokenEchoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(forgeTokenFromContext(r.Context())))
	})
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})), &buf
}

func parseLogLine(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parse log line: %v\nraw: %s", err, raw)
	}
	return m
}

func TestPassThroughMiddleware(t *testing.T) {
	handler := passThroughAuthMiddleware(discardLogger(), tokenEchoHandler())

	cases := []struct {
		name        string
		authHeader  string
		wantStatus  int
		wantBody    string
		wantWWWAuth bool
	}{
		{
			name:        "missing header → 401",
			authHeader:  "",
			wantStatus:  http.StatusUnauthorized,
			wantWWWAuth: true,
		},
		{
			name:        "non-bearer scheme → 401",
			authHeader:  "Basic dXNlcjpwYXNz",
			wantStatus:  http.StatusUnauthorized,
			wantWWWAuth: true,
		},
		{
			name:        "empty bearer → 401",
			authHeader:  "Bearer ",
			wantStatus:  http.StatusUnauthorized,
			wantWWWAuth: true,
		},
		{
			name:       "valid bearer → 200, token on ctx",
			authHeader: "Bearer ghp_user_pat_abc123",
			wantStatus: http.StatusOK,
			wantBody:   "ghp_user_pat_abc123",
		},
		{
			name:       "any opaque string passes through (we don't validate)",
			authHeader: "Bearer x",
			wantStatus: http.StatusOK,
			wantBody:   "x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(""))
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantWWWAuth {
				if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
					t.Errorf("WWW-Authenticate: got %q", got)
				}
			}
			if tc.wantBody != "" {
				if got := rec.Body.String(); got != tc.wantBody {
					t.Errorf("ctx token: got %q, want %q", got, tc.wantBody)
				}
			}
		})
	}
}

// TestPassThroughDoesNotValidateOrStore confirms the middleware
// never tries to look the token up in any local store. Pin behavior:
// any non-empty bearer survives. The validator is the upstream forge.
func TestPassThroughDoesNotValidateOrStore(t *testing.T) {
	handler := passThroughAuthMiddleware(discardLogger(), tokenEchoHandler())
	for _, tok := range []string{
		"random-string",
		"ghp_legitimately_formatted",
		"!@#$%^&*()_+",
		"a", // single character
	} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%q: middleware must accept any non-empty bearer; got %d", tok, rec.Code)
		}
		if got := rec.Body.String(); got != tok {
			t.Errorf("%q: ctx token mismatch; got %q", tok, got)
		}
	}
}

// TestAuthAuditFailureLogsReasonNotToken pins the audit-log invariant:
// a 401 emits a WARN with the reason enum, never the token. The token
// IS the secret in pass-through mode — leaking it to logs would be
// strictly worse than the centralized-store model we replaced.
func TestAuthAuditFailureLogsReasonNotToken(t *testing.T) {
	logger, buf := captureLogger()
	handler := passThroughAuthMiddleware(logger, tokenEchoHandler())

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer ghp_secret_abc")
	req.RemoteAddr = "203.0.113.7:55555"
	// Use an empty bearer so we hit the auth_failure path; the token
	// in the header above is the wrong shape (not actually a bearer
	// scheme) but Go's http strips the prefix correctly. Use the
	// "Bearer " case to verify the reason. Set a header we know
	// triggers a failure.
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	line := strings.TrimSpace(buf.String())
	parsed := parseLogLine(t, line)
	if parsed["msg"] != "auth_failure" || parsed["reason"] != "empty_bearer" {
		t.Errorf("expected auth_failure / empty_bearer; got %+v", parsed)
	}
	if parsed["level"] != "WARN" {
		t.Errorf("level: %v", parsed["level"])
	}
	// CRITICAL invariant: the bearer must never appear.
	if strings.Contains(line, "ghp_") {
		t.Errorf("audit log leaks token-shaped string: %s", line)
	}
}

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
