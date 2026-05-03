package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
)

// healthzHandler returns 200 "ok" while the process is alive. The
// orchestrator (Coolify, Kubernetes, ECS) polls this on a short
// interval to decide whether to restart the container; "alive but
// degraded" stays 200 here. Readiness (whether the upstream forge is
// reachable) is the readyz endpoint's job, not this one — splitting
// liveness from readiness is the standard pattern and lets a brief
// forge outage NOT cause restart flapping.
//
// No auth: orchestrators don't carry credentials, and the response
// body is intentionally trivial — no detail useful to an attacker.
func healthzHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

// readyzHandler returns 200 "ready" while the listener is bound. It
// is liveness-equivalent for a stateless protocol-translation
// daemon — there's no in-process state to "warm up" beyond the HTTP
// listener accepting connections, which the request reaching this
// handler proves.
//
// Earlier versions made an authenticated upstream forge call here
// (Whoami) using the host's credentials. That was unsafe in two
// ways and is fixed by #139:
//
//  1. Unauthenticated rate-limit drain: any unauthenticated peer
//     who could reach /readyz drained the host's forge rate limit
//     one request per probe; an attacker on the public internet
//     with a botnet could starve every legitimate gaia-mcp call
//     until the rate-limit window reset.
//
//  2. Pass-through-auth invariant violation: the HTTP transport's
//     selling point is "gaia-mcp stores nothing — the bearer the
//     client sends IS the forge PAT." A host PAT existed on disk
//     just to power /readyz, and a multi-user host or backup leak
//     of that file gave the attacker a real PAT, not just whatever
//     the most-recent caller sent.
//
// Operators who want to monitor "is the forge reachable from this
// gaia-mcp host?" point their probe at /readyz/upstream instead;
// that endpoint requires a bearer (the operator's monitoring
// credential), uses the supplied bearer as the forge token, and is
// rate-limited by the requester's own quota — not the host's.
//
// Detail-free body so the endpoint isn't a reconnaissance vector.
func readyzHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
}

// readyzUpstreamHandler returns 200 "ready" when the upstream forge
// is reachable AND the bearer the caller supplied is valid; 503
// "unready" on either failure. The handler is mounted INSIDE
// passThroughAuthMiddleware so an unauthenticated request never
// reaches it — the middleware returns 401, no upstream call is
// made, no rate-limit budget consumed.
//
// The forge token used for the upstream Whoami is the per-request
// bearer (via forgeTokenFromContext), not a host credential. Each
// caller spends only their own quota. Combined with the bearer
// requirement, this means an attacker who wants to drain a forge
// quota via this endpoint must already have a valid bearer for
// that account — at which point they have nominally-equivalent
// access already.
func readyzUpstreamHandler(buildProvider func(context.Context) (provider.Provider, error), logger *slog.Logger, timeout time.Duration) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		p, err := buildProvider(ctx)
		if err != nil {
			logger.Warn("readyz_upstream_unready", "reason", "build_provider", "err", err.Error())
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unready"))
			return
		}
		if _, err := p.Whoami(ctx); err != nil {
			logger.Warn("readyz_upstream_unready", "reason", "forge_ping", "err", err.Error())
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
}
