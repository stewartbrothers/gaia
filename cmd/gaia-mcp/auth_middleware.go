package main

import (
	"context"
	"crypto/subtle"
	"log/slog"
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
func bearerAuthMiddleware(tokens tokenStore, logger *slog.Logger, next http.Handler) http.Handler {
	if len(tokens) == 0 {
		return next
	}
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		label, reason, ok := authenticate(r, tokens)
		if !ok {
			logger.Warn("auth_failure",
				"reason", reason,
				"remote", clientAddr(r),
				"path", r.URL.Path,
			)
			w.Header().Set("WWW-Authenticate", `Bearer realm="gaia-mcp"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		logger.Info("auth_success",
			"label", label,
			"remote", clientAddr(r),
			"path", r.URL.Path,
		)
		ctx := context.WithValue(r.Context(), ctxKeyTokenLabel, label)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// clientAddr returns the request's source address for the audit log.
// Honors X-Forwarded-For when present (reverse-proxy deployments),
// otherwise falls back to the TCP peer.
//
// Single-IP only: the leftmost element of XFF, which the closest
// proxy MUST be configured to set correctly. Trusting a multi-element
// XFF blindly would let a client spoof their address by setting it
// themselves; the proxy is responsible for stripping client-supplied
// XFF and setting just the immediate peer.
func clientAddr(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma >= 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	return r.RemoteAddr
}

// authenticate extracts the bearer token from the request, looks it
// up in the store with constant-time equality, and returns the
// resolved label. Returns label, a stable reason string for audit
// logs (never the token itself), and ok=false on any failure —
// caller produces the 401 response.
//
// Constant-time comparison prevents timing oracles that could leak
// token prefixes. This matters because bearer tokens are
// long-lived; a single guess is cheap, but timing-based prefix
// recovery would let an attacker iterate efficiently.
func authenticate(r *http.Request, tokens tokenStore) (label, reason string, ok bool) {
	authz := r.Header.Get("Authorization")
	if authz == "" {
		return "", "no_authorization_header", false
	}
	if !strings.HasPrefix(authz, "Bearer ") {
		return "", "non_bearer_scheme", false
	}
	supplied := strings.TrimPrefix(authz, "Bearer ")
	if supplied == "" {
		return "", "empty_bearer", false
	}
	suppliedB := []byte(supplied)
	for token, lbl := range tokens {
		if subtle.ConstantTimeCompare(suppliedB, []byte(token)) == 1 {
			return lbl, "", true
		}
	}
	return "", "unknown_token", false
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
