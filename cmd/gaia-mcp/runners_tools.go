package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/provider"
)

func registerRunnersTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_runners_list",
		mcp.WithDescription("List the self-hosted Actions runners registered on a repo (or the owner's org with org=true). Returns each runner's name, status (online/offline), busy flag, and labels — to confirm a CI/deploy runner is live and what it can run. The repo-level list may be empty when runners are org/instance-scoped; use org=true then."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithBoolean("org", mcp.Description("list the owner's org-level runners instead of the repo's")),
		mcp.WithNumber("limit", mcp.Description("max runners to return")),
		mcp.WithString("cursor", mcp.Description("opaque pagination cursor from a previous response")),
	), ctxBoundHandler(handleRunnersList))
}

func handleRunnersList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	runners, page, err := p.ListRunners(ctx, owner, repo, provider.ListRunnersOptions{
		Org:    optBool(args, "org"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(runners, page), nil
}
