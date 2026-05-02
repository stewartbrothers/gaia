package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

// bindPolicy holds the security-relevant subset of httpConfig: the
// listen address, whether auth tokens are configured, and whether the
// operator explicitly accepted public-no-auth (e.g., behind a reverse
// proxy that handles auth itself).
//
// Lives in its own file because the policy is narrow and tested in
// isolation — a security check should never be hidden behind several
// layers of plumbing.
type bindPolicy struct {
	Addr              string
	HasAuth           bool
	AllowPublicNoAuth bool
}

// validate refuses to start the listener if the operator has bound to
// a non-loopback interface without auth and without the explicit
// opt-out. This is the central rule: anyone who can reach the socket
// can act as the configured forge token holder, so unauthenticated
// public exposure is a deploy-time foot-gun, not a feature.
//
// The allowed combinations:
//
//	loopback (127.0.0.1, [::1], localhost) + no auth   → ok
//	loopback                                + auth     → ok (over-cautious is fine)
//	non-loopback                            + auth     → ok
//	non-loopback + no auth + AllowPublicNoAuth=true    → ok (proxy in front)
//	non-loopback + no auth + AllowPublicNoAuth=false   → REFUSED
func (p bindPolicy) validate() error {
	loopback, err := isLoopbackBind(p.Addr)
	if err != nil {
		return exitcode.Wrap(err, exitcode.Usage, "parse --http address")
	}
	if loopback {
		return nil
	}
	if p.HasAuth {
		return nil
	}
	if p.AllowPublicNoAuth {
		return nil
	}
	return exitcode.Errorf(exitcode.Usage,
		"--http %q binds to a non-loopback interface without auth; "+
			"add --token-file <path> to enforce bearer auth, or pass "+
			"--allow-public-no-auth if a reverse proxy already authenticates "+
			"requests in front of gaia-mcp", p.Addr)
}

// isLoopbackBind returns true if addr resolves to a loopback-only
// listener (127.0.0.1, ::1, "localhost"). An empty host (":8080" or
// "0.0.0.0:...") binds to all interfaces — that's *not* loopback.
//
// Reachability is decided by the host portion alone; the port doesn't
// matter for the policy. Returns an error on a malformed address so
// the operator gets a clear message before the listener tries to come
// up and prints a less helpful net.OpError.
func isLoopbackBind(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("malformed addr %q: %w", addr, err)
	}
	host = strings.TrimSpace(host)
	if host == "" {
		// ":8080" parses as host="", port="8080"; binds to all
		// interfaces, NOT loopback.
		return false, nil
	}
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname other than "localhost" — we can't tell without
		// resolving DNS, and DNS-at-startup adds a failure mode. Treat
		// as non-loopback. The operator can still bind explicitly to
		// 127.0.0.1 if they meant local-only.
		return false, nil
	}
	return ip.IsLoopback(), nil
}
