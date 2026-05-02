package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// passThroughHandler echoes the resolved token label so tests can
// assert middleware passed it through correctly.
func labelEchoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(labelFromContext(r.Context())))
	})
}

func TestBearerAuthMiddleware(t *testing.T) {
	tokens := tokenStore{
		"tok_alice": "alice",
		"tok_bob":   "bob",
	}
	handler := bearerAuthMiddleware(tokens, labelEchoHandler())

	cases := []struct {
		name        string
		authHeader  string
		wantStatus  int
		wantBody    string
		wantWWWAuth bool
	}{
		{
			name:        "missing header → 401 with WWW-Authenticate",
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
			name:        "wrong token → 401",
			authHeader:  "Bearer tok_eve",
			wantStatus:  http.StatusUnauthorized,
			wantWWWAuth: true,
		},
		{
			name:       "valid alice → 200 with label on ctx",
			authHeader: "Bearer tok_alice",
			wantStatus: http.StatusOK,
			wantBody:   "alice",
		},
		{
			name:       "valid bob → 200 with label on ctx",
			authHeader: "Bearer tok_bob",
			wantStatus: http.StatusOK,
			wantBody:   "bob",
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
				body, _ := io.ReadAll(rec.Body)
				// 401 body must be opaque — no detail about why.
				if strings.Contains(string(body), "tok_") {
					t.Errorf("401 body leaks token shape: %q", body)
				}
			}
			if tc.wantBody != "" {
				if got := rec.Body.String(); got != tc.wantBody {
					t.Errorf("body: got %q, want %q", got, tc.wantBody)
				}
			}
		})
	}
}

func TestBearerAuthMiddlewarePassThroughWhenNoTokens(t *testing.T) {
	// With no tokens configured, the middleware is a no-op (the bind
	// policy already gated this — only loopback or
	// --allow-public-no-auth reaches here).
	handler := bearerAuthMiddleware(nil, labelEchoHandler())
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (pass-through)", rec.Code)
	}
	// Label is empty — no auth happened.
	if rec.Body.String() != "" {
		t.Errorf("expected empty label; got %q", rec.Body.String())
	}
}

// TestBearerAuthMiddlewareConstantTime is a smoke test that the
// timing-attack mitigation (subtle.ConstantTimeCompare) is wired up.
// Doesn't measure timing — that would be flaky on shared CI — but
// confirms the function still works for tokens with shared prefixes
// and fails uniformly for non-matches.
func TestBearerAuthMiddlewareConstantTime(t *testing.T) {
	tokens := tokenStore{
		"abcdef_long_token_alice": "alice",
		"abcdef_long_token_bob":   "bob",
	}
	handler := bearerAuthMiddleware(tokens, labelEchoHandler())
	for _, attempt := range []string{
		"abcdef_long_token_alice", // valid
		"abcdef_long_token_bob",   // valid
		"abcdef_long_token_eve",   // invalid, prefix-shared
		"abcdef_long_token_X",     // invalid, single-byte off
	} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+attempt)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if _, ok := tokens[attempt]; ok {
			if rec.Code != http.StatusOK {
				t.Errorf("%q should auth; got %d", attempt, rec.Code)
			}
		} else {
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%q should fail; got %d", attempt, rec.Code)
			}
		}
	}
}
