// Command gaia-mcp serves an MCP stdio server exposing gaia's
// operations as MCP tools. Phase 1 (#26 + #27) lands the read +
// write tool surface; Phase 3 (#39) will add HTTP/SSE transport for
// remote consumers.
//
// Layered config + credentials are resolved the same way the CLI
// resolves them (via internal/forgebuilder), so a user who has run
// `gaia auth forgejo <url>` against the same machine can immediately
// connect this binary to an MCP-aware host with no extra setup.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/forgebuilder"
	"github.com/stewartbrothers/gaia/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gaia-mcp:", err)
		os.Exit(exitcode.Of(err))
	}
}

func run() error {
	s := server.NewMCPServer(
		"gaia-mcp",
		version.Version,
		server.WithToolCapabilities(false),
	)

	registerSmokeTools(s)

	return server.ServeStdio(s)
}

// registerSmokeTools registers the always-on tools that don't need
// a forge connection (`gaia.version`) and the simplest one that does
// (`gaia.whoami`). Read/write tools land with #27; this scaffold
// proves the transport, schema, and forge resolution plumbing.
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
	p, info, err := forgebuilder.Build(forgebuilder.Override{})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	login, err := p.Whoami(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := fmt.Sprintf(`{"login":%q,"provider":%q,"host":%q}`,
		login, info.Provider, info.Host)
	return mcp.NewToolResultText(out), nil
}
