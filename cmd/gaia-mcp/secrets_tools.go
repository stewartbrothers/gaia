package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/provider"
)

func registerSecretsTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_secrets_list",
		mcp.WithDescription("List the CI/Actions secrets configured on a repo (or the owner's org with org=true). Returns names + timestamps only — secret VALUES are never exposed by the forge API. Use to confirm which secrets are set up (e.g. a release workflow's deploy key)."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithBoolean("org", mcp.Description("list the owner's org-level secrets instead of the repo's")),
		mcp.WithNumber("limit", mcp.Description("max secrets to return")),
		mcp.WithString("cursor", mcp.Description("opaque pagination cursor from a previous response")),
	), ctxBoundHandler(handleSecretsList))
}

func handleSecretsList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	secrets, page, err := p.ListSecrets(ctx, owner, repo, provider.ListSecretsOptions{
		Org:    optBool(args, "org"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(secrets, page), nil
}
