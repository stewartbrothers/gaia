package main

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// ctxKeyForgeToken keys a request's forge token on its context. The
// HTTP transport extracts the bearer from `Authorization: Bearer ...`
// and stores it here; tool handlers' build(ctx) reads it and
// constructs a per-request provider with that token.
//
// gaia-mcp itself never stores forge credentials. The bearer the
// client sends *is* the forge PAT — pass-through, no local validation,
// no central store. Anyone who steals a token from gaia-mcp gets
// nothing because there's nothing at rest to steal.
type ctxKey string

const ctxKeyForgeToken ctxKey = "gaia.forge.token"

// passThroughAuthMiddleware extracts the bearer from the
// Authorization header, stores it on the request context, and lets
// the request through. It is the *only* auth gate gaia-mcp owns: a
// missing or malformed bearer fails the request because there's no
// credential to use upstream.
//
// On the wire, gaia-mcp NEVER validates the bearer — that's the
// upstream forge's job. We just transport it. If the token is
// invalid, the upstream forge will return 401 and the tool handler
// surfaces that to the MCP client.
//
// 401 here means "the request lacks a usable bearer." Detail-free
// body so the endpoint doesn't become a probing oracle.
func passThroughAuthMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, reason, ok := extractBearer(r)
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
		ctx := context.WithValue(r.Context(), ctxKeyForgeToken, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractBearer returns the bearer value, a stable reason string for
// audit logs (never the token itself), and ok=false on any failure.
func extractBearer(r *http.Request) (token, reason string, ok bool) {
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
	return supplied, "", true
}

// forgeTokenFromContext returns the per-request forge token attached
// by passThroughAuthMiddleware, or "" if the request didn't carry
// one. Stdio mode never attaches; the empty return tells build() to
// fall back to layered credentials.
func forgeTokenFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyForgeToken).(string); ok {
		return v
	}
	return ""
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
