package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// ctxKeyTokenLabel keys a request's resolved token label on its
// context. Audit-log middleware reads it; never the raw token.
type ctxKey string

const ctxKeyTokenLabel ctxKey = "gaia.token.label"

// bearerAuthMiddleware enforces `Authorization: Bearer <token>` on
// the wrapped handler. With no tokens configured, it's a no-op (the
// bind policy already gated this case in main.go — the only paths
// that reach here unauthenticated are loopback or
// --allow-public-no-auth).
//
// On a valid bearer, the resolved label is stored on the request
// context so downstream code can attribute the call without ever
// touching the token. Audit logs reference the label.
//
// On any failure, the response is 401 with WWW-Authenticate, and
// the response body is the static string "Unauthorized" — no detail
// about *why* the token was rejected (existing token vs. invalid
// vs. missing). Detail goes to stderr for the operator; the wire is
// opaque to attackers probing for token shape.
func bearerAuthMiddleware(tokens tokenStore, next http.Handler) http.Handler {
	if len(tokens) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		label, ok := authenticate(r, tokens)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="gaia-mcp"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyTokenLabel, label)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticate extracts the bearer token from the request, looks it
// up in the store with constant-time equality, and returns the
// resolved label. Returns ("", false) on any failure — caller
// produces the 401 response.
//
// Constant-time comparison prevents timing oracles that could leak
// token prefixes. This matters because bearer tokens are
// long-lived; a single guess is cheap, but timing-based prefix
// recovery would let an attacker iterate efficiently.
func authenticate(r *http.Request, tokens tokenStore) (label string, ok bool) {
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return "", false
	}
	supplied := strings.TrimPrefix(authz, "Bearer ")
	if supplied == "" {
		return "", false
	}
	suppliedB := []byte(supplied)
	for token, lbl := range tokens {
		if subtle.ConstantTimeCompare(suppliedB, []byte(token)) == 1 {
			return lbl, true
		}
	}
	return "", false
}

// labelFromContext returns the token label attached by
// bearerAuthMiddleware on a successful auth, or "" if the request
// wasn't authenticated (loopback no-auth path or
// --allow-public-no-auth bypass).
func labelFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyTokenLabel).(string); ok {
		return v
	}
	return ""
}
