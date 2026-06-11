package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerBranchTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_branch_protection_get",
		mcp.WithDescription("Show a branch's protection rule: required status-check contexts, the strict up-to-date flag, and required approvals."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("branch", mcp.Required()),
	), ctxBoundHandler(handleBranchProtectionGet))

	s.AddTool(mcp.NewTool("gaia_branch_protection_set",
		mcp.WithDescription("Create or replace a branch's protection rule. Declarative: the fields fully specify the rule (omitting required_checks clears them)."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("branch", mcp.Required()),
		mcp.WithArray("required_checks", mcp.Description("status-check contexts that must pass before merge (e.g. \"CI / Build\")")),
		mcp.WithBoolean("strict", mcp.Description("require the branch be up to date with base before merge")),
		mcp.WithNumber("required_approvals", mcp.Description("number of approving reviews required to merge")),
	), ctxBoundHandler(handleBranchProtectionSet))

	s.AddTool(mcp.NewTool("gaia_branch_protection_delete",
		mcp.WithDescription("Remove a branch's protection rule."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("branch", mcp.Required()),
	), ctxBoundHandler(handleBranchProtectionDelete))
}

func branchFromArgs(args map[string]any) (owner, repo, branch string, err error) {
	owner, repo, err = repoFromArgs(args)
	if err != nil {
		return "", "", "", err
	}
	branch = optString(args, "branch")
	if branch == "" {
		return "", "", "", exitcode.Errorf(exitcode.Usage, "branch is required")
	}
	return owner, repo, branch, nil
}

func handleBranchProtectionGet(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, branch, err := branchFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	bp, err := p.GetBranchProtection(ctx, owner, repo, branch)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(bp, nil), nil
}

func handleBranchProtectionSet(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, branch, err := branchFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	bp, err := p.SetBranchProtection(ctx, owner, repo, branch, provider.SetBranchProtectionOptions{
		RequiredStatusChecks: optStringSlice(args, "required_checks"),
		StrictStatusChecks:   optBool(args, "strict"),
		RequiredApprovals:    optInt(args, "required_approvals"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(bp, nil), nil
}

func handleBranchProtectionDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, branch, err := branchFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.DeleteBranchProtection(ctx, owner, repo, branch); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"deleted": true, "branch": branch}, nil), nil
}
