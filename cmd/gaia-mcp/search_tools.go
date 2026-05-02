package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerSearchTool(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_search",
		mcp.WithDescription("Search issues + PRs. Empty repo searches across every repo the token can see."),
		mcp.WithString("query", mcp.Required()),
		mcp.WithString("repo", mcp.Description("owner/name; omit for cross-repo search")),
		mcp.WithArray("kinds", mcp.Description("issue | pull_request"), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleSearch))
}

func handleSearch(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	query := optString(args, "query")
	if query == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "query is required")), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	results, page, err := p.Search(ctx, query, provider.SearchOptions{
		Kinds:  optStringSlice(args, "kinds"),
		Repo:   optString(args, "repo"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(results, page), nil
}
