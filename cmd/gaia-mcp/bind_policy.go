package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

// bindPolicy holds the security-relevant subset of httpConfig: the
// listen address and whether the operator has acknowledged that TLS
// terminates upstream of gaia-mcp.
//
// Under pass-through auth, every HTTP request must carry the
// caller's forge PAT in `Authorization: Bearer …`. If the listener
// is on a non-loopback interface without TLS in front of it, those
// PATs cross the wire in cleartext — at which point a passive
// observer on the network owns every caller's forge identity. The
// policy refuses that combination unless the operator explicitly
// confirms that TLS termination is upstream (reverse proxy, k8s
// ingress, mesh sidecar, etc.).
//
// gaia-mcp itself is HTTP, not HTTPS — terminating TLS in-process
// would mean shipping a cert handler plus rotation logic for
// dubious value when every realistic deploy already has nginx /
// Caddy / Traefik / Cloudflare in front.
type bindPolicy struct {
	Addr             string
	AllowPublicNoTLS bool
}

// validate refuses to start the listener on a non-loopback address
// without the explicit TLS-in-front acknowledgment. The allowed
// combinations:
//
//	loopback (127.0.0.1, [::1], localhost)              → ok
//	non-loopback + AllowPublicNoTLS=true (proxy in front) → ok
//	non-loopback + AllowPublicNoTLS=false (default)     → REFUSED
func (p bindPolicy) validate() error {
	loopback, err := isLoopbackBind(p.Addr)
	if err != nil {
		return exitcode.Wrap(err, exitcode.Usage, "parse --http address")
	}
	if loopback {
		return nil
	}
	if p.AllowPublicNoTLS {
		return nil
	}
	return exitcode.Errorf(exitcode.Usage,
		"--http %q binds to a non-loopback interface; under pass-through auth "+
			"every request carries the caller's forge PAT in Authorization: Bearer, "+
			"so TLS must terminate upstream. Pass --allow-public-no-tls only if a "+
			"reverse proxy (nginx, Caddy, k8s ingress) handles TLS in front of "+
			"gaia-mcp", p.Addr)
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
