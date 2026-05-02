// Command gaia-mcp serves an MCP server exposing gaia's operations
// as MCP tools. Two transports:
//
//   - stdio (default): for subprocess hosts (Claude Desktop, Cursor,
//     etc.). Single-tenant — uses the same layered config + credential
//     resolution as `gaia` itself.
//
//   - HTTP (--http :addr): streamable-HTTP transport per the
//     2025-03-26 spec, for remote agents that want a long-running
//     daemon they can pin one URL at. Single-tenant in this commit;
//     #40 layers per-request bearer-token auth on top.
//
// Layered config + credentials are resolved the same way the CLI
// resolves them (via internal/forgebuilder), so a user who has run
// `gaia auth forgejo <url>` against the same machine can immediately
// connect this binary to an MCP-aware host with no extra setup.
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
	// AllowPublicNoAuth lets the operator bind to a non-loopback
	// address without configuring bearer tokens. Required when a
	// reverse proxy handles auth in front of gaia-mcp; otherwise
	// startup refuses with a usage error to prevent accidental
	// public exposure.
	AllowPublicNoAuth bool
	// TokenFile points at a one-token-per-line file. When non-empty,
	// every HTTP request must carry a matching `Authorization: Bearer
	// <token>`; the bind policy then permits non-loopback exposure.
	TokenFile string
}

func run(args []string) error {
	fs := flag.NewFlagSet("gaia-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := httpConfig{}
	fs.StringVar(&cfg.Addr, "http", "", "if set, serve HTTP streamable-MCP on the given address (e.g. :8080); default is stdio")
	fs.StringVar(&cfg.BasePath, "base-path", "/mcp", "URL path prefix for the HTTP transport")
	fs.DurationVar(&cfg.ReadHeaderTimeout, "read-header-timeout", 10*time.Second, "max time to read request headers (slow-loris guard)")
	fs.DurationVar(&cfg.IdleTimeout, "idle-timeout", 120*time.Second, "max idle time between requests on a keep-alive connection")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", 10*time.Second, "max drain window on SIGTERM/SIGINT before in-flight requests are cut")
	fs.BoolVar(&cfg.AllowPublicNoAuth, "allow-public-no-auth", false, "permit binding to a non-loopback interface without bearer auth (only safe behind a reverse proxy that authenticates requests itself)")
	fs.StringVar(&cfg.TokenFile, "token-file", "", "path to a file with one bearer token per line (mode must be 0600); enables Authorization: Bearer auth on the HTTP transport")
	if err := fs.Parse(args); err != nil {
		return err
	}

	s := buildServer()

	if cfg.Addr != "" {
		tokens, err := loadTokensFromFile(cfg.TokenFile)
		if err != nil {
			return err
		}
		policy := bindPolicy{
			Addr:              cfg.Addr,
			HasAuth:           len(tokens) > 0,
			AllowPublicNoAuth: cfg.AllowPublicNoAuth,
		}
		if err := policy.validate(); err != nil {
			return err
		}
		return runHTTP(cfg, s, tokens)
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
func runHTTP(cfg httpConfig, s *server.MCPServer, tokens tokenStore) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Use the mcp-go streamable-HTTP server as a plain http.Handler
	// and host it ourselves so we own the timeouts. mcp-go's
	// internal http.Server has no timeouts set — fine for tests, bad
	// for a public daemon (slow-loris exposure, dangling connections).
	streamable := server.NewStreamableHTTPServer(s)
	authed := bearerAuthMiddleware(tokens, streamable)
	mux := http.NewServeMux()
	mux.Handle(cfg.BasePath, authed)

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
			"auth", len(tokens) > 0,
			"token_count", len(tokens),
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
	p, err := build()
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
