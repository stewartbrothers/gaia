// Command gaia-mcp serves an MCP server exposing gaia's operations
// as MCP tools. Two transports:
//
//   - stdio (default): for subprocess hosts (Claude Desktop, Cursor,
//     etc.). Single-tenant — uses the same layered config + credential
//     resolution as `gaia` itself, with the host-provided forge token.
//
//   - HTTP (--http :addr): streamable-HTTP transport per the
//     2025-03-26 spec, with **pass-through auth**: every request
//     must carry the caller's own forge PAT in
//     `Authorization: Bearer …`. gaia-mcp uses that token verbatim
//     for the upstream call and stores nothing — the daemon is a
//     thin protocol-translation layer, not a credential broker.
//
// Pass-through means each agent acts as itself on the forge.
// There's no per-tenant routing because the bearer *is* the
// tenancy.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gaia-mcp:", err)
		os.Exit(exitcode.Of(err))
	}
}

// httpConfig groups the HTTP transport's tunable knobs. Defaults
// match container-deployment best practices: short header timeout
// (slow-loris guard), generous idle timeout (keep-alives across
// multiple JSON-RPC calls), 10s shutdown drain.
type httpConfig struct {
	Addr              string
	BasePath          string
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	// AllowPublicNoTLS lets the operator bind to a non-loopback
	// address. Required because every HTTP request carries the
	// caller's forge PAT in Authorization: Bearer; the operator must
	// confirm that TLS terminates upstream (reverse proxy, k8s
	// ingress, etc.) before bearer tokens cross the wire.
	AllowPublicNoTLS bool
}

func run(args []string) error {
	fs := flag.NewFlagSet("gaia-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := httpConfig{}
	fs.StringVar(&cfg.Addr, "http", "", "if set, serve HTTP streamable-MCP on the given address (e.g. 127.0.0.1:8080); default is stdio")
	fs.StringVar(&cfg.BasePath, "base-path", "/mcp", "URL path prefix for the HTTP transport")
	fs.DurationVar(&cfg.ReadHeaderTimeout, "read-header-timeout", 10*time.Second, "max time to read request headers (slow-loris guard)")
	fs.DurationVar(&cfg.IdleTimeout, "idle-timeout", 120*time.Second, "max idle time between requests on a keep-alive connection")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", 10*time.Second, "max drain window on SIGTERM/SIGINT before in-flight requests are cut")
	fs.BoolVar(&cfg.AllowPublicNoTLS, "allow-public-no-tls", false, "permit binding to a non-loopback interface without TLS termination (only safe behind a reverse proxy that handles TLS upstream)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	s := buildServer()

	if cfg.Addr != "" {
		policy := bindPolicy{
			Addr:             cfg.Addr,
			AllowPublicNoTLS: cfg.AllowPublicNoTLS,
		}
		if err := policy.validate(); err != nil {
			return err
		}
		return runHTTP(cfg, s)
	}
	return server.ServeStdio(s)
}

func buildServer() *server.MCPServer {
	s := server.NewMCPServer(
		"gaia-mcp",
		version.Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	registerSmokeTools(s)
	registerAllTools(s)
	return s
}

// runHTTP serves the streamable-HTTP transport with the configured
// timeouts. Listens for SIGTERM/SIGINT and shuts down gracefully —
// orchestrators (Coolify, Kubernetes, ECS) all send SIGTERM first
// then SIGKILL after a grace period, so honoring SIGTERM cleanly is
// what makes rolling deploys lossless.
func runHTTP(cfg httpConfig, s *server.MCPServer) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Use the mcp-go streamable-HTTP server as a plain http.Handler
	// and host it ourselves so we own the timeouts. mcp-go's
	// internal http.Server has no timeouts set — fine for tests, bad
	// for a public daemon (slow-loris exposure, dangling connections).
	streamable := server.NewStreamableHTTPServer(s)
	authed := passThroughAuthMiddleware(logger, streamable)
	mux := http.NewServeMux()
	mux.Handle(cfg.BasePath, authed)

	// /healthz and /readyz are mounted on the same listener so a
	// single port is the deploy contract. Both sit *outside* the
	// auth middleware — orchestrators don't carry bearer tokens, and
	// the responses are opaque (no credential surface).
	//
	// /healthz is liveness (process alive). /readyz is now also
	// liveness-equivalent for this stateless protocol-translation
	// daemon: the listener being bound is the only readiness signal
	// gaia-mcp owns. Earlier versions made an authenticated forge
	// round-trip here using the host's credentials — that's been
	// removed (#139) because (a) any unauthenticated peer could
	// drain the host's forge rate limit one probe at a time, and
	// (b) the host PAT existing at rest violated the
	// pass-through-auth invariant.
	//
	// Operators who want to monitor "forge reachable from this
	// gaia-mcp host" use /readyz/upstream, which is mounted INSIDE
	// the auth middleware; the bearer the probe carries is the
	// forge token used for the Whoami round-trip, so each caller
	// spends only their own quota.
	mux.Handle("/healthz", healthzHandler())
	mux.Handle("/readyz", readyzHandler())
	mux.Handle("/readyz/upstream", passThroughAuthMiddleware(logger,
		readyzUpstreamHandler(build, logger, 5*time.Second)))

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening",
			"addr", cfg.Addr, "path", cfg.BasePath,
			"read_header_timeout", cfg.ReadHeaderTimeout.String(),
			"idle_timeout", cfg.IdleTimeout.String(),
		)
		errCh <- httpSrv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown", "signal", sig.String(), "drain_timeout", cfg.ShutdownTimeout.String())
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return httpSrv.Shutdown(ctx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return exitcode.Wrap(err, exitcode.Network, "http server")
		}
		return nil
	}
}

// registerSmokeTools registers the always-on tools that don't need
// a forge connection (`gaia.version`) and the simplest one that does
// (`gaia.whoami`). Read/write tools are registered by registerAllTools.
func registerSmokeTools(s *server.MCPServer) {
	versionTool := mcp.NewTool("gaia_version",
		mcp.WithDescription("Print gaia-mcp version, commit, and Go runtime info."),
	)
	s.AddTool(versionTool, handleVersion)

	whoamiTool := mcp.NewTool("gaia_whoami",
		mcp.WithDescription("Return the authenticated user's login on the active forge. Confirms the configured token still works."),
	)
	s.AddTool(whoamiTool, handleWhoami)
}

func handleVersion(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	out := fmt.Sprintf(`{"version":%q,"commit":%q,"go_version":%q}`,
		version.Version, version.Commit, runtime.Version())
	return mcp.NewToolResultText(out), nil
}

func handleWhoami(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p, err := build(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	login, err := p.Whoami(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := fmt.Sprintf(`{"login":%q}`, login)
	return mcp.NewToolResultText(out), nil
}
