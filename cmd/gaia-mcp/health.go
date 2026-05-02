package main

import (
	"net/http"
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
