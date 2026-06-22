package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/provider"
)

func registerCollaboratorsTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_collaborators_list",
		mcp.WithDescription("List a repo's collaborators with their permission level — an access audit of who can touch the repo and at what level. GitHub returns the permission inline; Forgejo resolves it per-user."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("limit", mcp.Description("max collaborators to return")),
		mcp.WithString("cursor", mcp.Description("opaque pagination cursor from a previous response")),
	), ctxBoundHandler(handleCollaboratorsList))
}

func handleCollaboratorsList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	collaborators, page, err := p.ListCollaborators(ctx, owner, repo, provider.ListCollaboratorsOptions{
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(collaborators, page), nil
}
