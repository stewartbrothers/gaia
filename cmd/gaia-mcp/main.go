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

func run(args []string) error {
	fs := flag.NewFlagSet("gaia-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	httpAddr := fs.String("http", "", "if set, serve HTTP streamable-MCP on the given address (e.g. :8080); default is stdio")
	basePath := fs.String("base-path", "/mcp", "URL path prefix for the HTTP transport")
	if err := fs.Parse(args); err != nil {
		return err
	}

	s := buildServer()

	if *httpAddr != "" {
		return runHTTP(*httpAddr, *basePath, s)
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

// runHTTP serves the streamable-HTTP transport. Listens for
// SIGTERM/SIGINT and shuts down gracefully; the deadline mirrors the
// container-deploy convention of "10s to drain in-flight, then kill."
func runHTTP(addr, basePath string, s *server.MCPServer) error {
	httpServer := server.NewStreamableHTTPServer(s,
		server.WithEndpointPath(basePath),
	)

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "gaia-mcp: listening on %s%s\n", addr, basePath)
		errCh <- httpServer.Start(addr)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "gaia-mcp: received %s, shutting down\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
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
