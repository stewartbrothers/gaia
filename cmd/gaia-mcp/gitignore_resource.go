package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/internal/gitignore"
)

// registerGitignoreResource exposes the recommended .gitignore block
// as a static MCP resource at gaia://gitignore. Same `go:embed`
// source as `gaia gitignore` (internal/gitignore.Recommended) so
// CLI and MCP consumers can never disagree about the canonical
// list.
//
// Why a resource and not a tool: the block is read-only static
// content. Resources are exactly the protocol shape MCP defines for
// "here is some text the agent can fetch by URI"; tools are for
// side-effect-bearing operations.
//
// Registered from buildServer() so both stdio and streamable-HTTP
// transports expose the resource identically — the MCP server is
// transport-agnostic, and resource registration lives on the same
// MCPServer instance the transports wrap.
func registerGitignoreResource(s *server.MCPServer) {
	r := mcp.NewResource(
		"gaia://gitignore",
		"gitignore",
		mcp.WithMIMEType("text/plain"),
		mcp.WithResourceDescription("Recommended .gitignore entries for projects using gaia (credentials store + Phase 9 insights paths)."),
	)
	s.AddResource(r, handleGitignoreResource)
}

// handleGitignoreResource serves the embedded recommended block
// verbatim. The handler ignores the request URI beyond the
// registration match — gaia exposes exactly one URI for this
// resource, and there is no parameterisation.
func handleGitignoreResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "text/plain",
			Text:     gitignore.Recommended,
		},
	}, nil
}
