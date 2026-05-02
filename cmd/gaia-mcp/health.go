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

// readyzHandler returns 200 "ready" when the upstream forge is
// reachable AND the configured token is still valid; 503 "unready"
// otherwise. Orchestrators use readyz to decide whether to send
// traffic — a 503 takes the pod out of rotation without restarting
// it, which is the right move when the dependency is the problem
// rather than the daemon itself.
//
// The check: build the provider (resolves config + credentials) and
// call Whoami with a hard timeout. Whoami is the cheapest forge
// round-trip; we're proving "auth + network OK", not exercising any
// data path.
//
// Detail goes to stderr (operator-visible); wire body is opaque
// ("ready"/"unready") so the endpoint isn't a reconnaissance vector.
func readyzHandler(buildProvider func(context.Context) (provider.Provider, error), logger *slog.Logger, timeout time.Duration) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		// readyz uses the operator's host-side credentials (no
		// per-request bearer attached to ctx, since orchestrator
		// probes don't carry one). The forgebuilder fall-through
		// resolves the layered credential store.
		p, err := buildProvider(ctx)
		if err != nil {
			logger.Warn("readyz_unready", "reason", "build_provider", "err", err.Error())
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unready"))
			return
		}
		if _, err := p.Whoami(ctx); err != nil {
			logger.Warn("readyz_unready", "reason", "forge_ping", "err", err.Error())
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
}
