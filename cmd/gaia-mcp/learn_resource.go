package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	agentguide "github.com/stewartbrothers/gaia"
)

// registerLearnResource exposes the embedded agent-onboarding guide
// as a static MCP resource at gaia://learn. Same `go:embed` source
// as `gaia learn` (agentguide.Markdown, embedded from
// docs/agent-guide.md at module root) so CLI and MCP consumers can
// never disagree about the canonical briefing.
//
// Why a resource and not a tool: the guide is read-only static
// content. Resources are exactly the protocol shape MCP defines for
// "here is some text the agent can fetch by URI"; tools are for
// side-effect-bearing operations.
//
// Registered from buildServer() so both stdio and streamable-HTTP
// transports expose the resource identically — the MCP server is
// transport-agnostic, and resource registration lives on the same
// MCPServer instance the transports wrap. Mirrors the gitignore
// resource pattern (cmd/gaia-mcp/gitignore_resource.go in PR #273)
// down to file naming and call shape.
func registerLearnResource(s *server.MCPServer) {
	r := mcp.NewResource(
		"gaia://learn",
		"learn",
		mcp.WithMIMEType("text/markdown"),
		mcp.WithResourceDescription("Quick-start guide for AI coding agents using gaia."),
	)
	s.AddResource(r, handleLearnResource)
}

// handleLearnResource serves the embedded agent guide markdown
// verbatim. The handler ignores the request URI beyond the
// registration match — gaia exposes exactly one URI for this
// resource, and there is no parameterisation.
func handleLearnResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "text/markdown",
			Text:     agentguide.Markdown,
		},
	}, nil
}
