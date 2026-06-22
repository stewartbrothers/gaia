package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/provider"
)

func registerVariablesTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_variables_list",
		mcp.WithDescription("List the CI/Actions variables configured on a repo (or the owner's org with org=true). Unlike secrets, variable VALUES are non-secret config (e.g. TURBO_TEAM, TURBO_API) and ARE returned. Use to inspect which variables are set up and to what."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithBoolean("org", mcp.Description("list the owner's org-level variables instead of the repo's")),
		mcp.WithNumber("limit", mcp.Description("max variables to return")),
		mcp.WithString("cursor", mcp.Description("opaque pagination cursor from a previous response")),
	), ctxBoundHandler(handleVariablesList))
}

func handleVariablesList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	variables, page, err := p.ListVariables(ctx, owner, repo, provider.ListVariablesOptions{
		Org:    optBool(args, "org"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(variables, page), nil
}
