package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// registerIssueDepTools exposes Forgejo's issue-dependency surface
// as three MCP tools. Mirrors the CLI shape from PR 2 of #317:
//
//	gaia_issue_dep_list   — list blockers (default) or blocks
//	gaia_issue_dep_add    — add an edge via --blocker or --blocks framing
//	gaia_issue_dep_remove — remove an edge
//
// "X blocks Y" and "Y depends on X" describe the same edge; both
// framings (blocker / blocks) map to the same AddIssueDependency
// call after host/target swap. GitHub provider returns
// NotImplemented per docs/provider-contract.md.
func registerIssueDepTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_issue_dep_list",
		mcp.WithDescription("List issue dependencies (blockers) or blocks. Returns trimmed Issue records. Forgejo + GitHub (REST, API version 2026-03-10)."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("number", mcp.Required(), mcp.Description("issue number")),
		mcp.WithString("direction", mcp.Description(`"blockers" (default — issues blocking this one) or "blocks" (issues this one blocks)`)),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleIssueDepList))

	s.AddTool(mcp.NewTool("gaia_issue_dep_add",
		mcp.WithDescription(`Add a dependency edge. Two framings (mutually exclusive): blocker=M means "M blocks N" (where N is the `+"`number`"+` arg); blocks=M means "N blocks M". Same edge, opposite direction of argument flow. Forgejo + GitHub.`),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required(), mcp.Description("the issue the edge is anchored to")),
		mcp.WithNumber("blocker", mcp.Description(`M where "M blocks N"`)),
		mcp.WithNumber("blocks", mcp.Description(`M where "N blocks M"`)),
	), ctxBoundHandler(handleIssueDepAdd))

	s.AddTool(mcp.NewTool("gaia_issue_dep_remove",
		mcp.WithDescription("Remove a dependency edge. Same blocker/blocks framing as gaia_issue_dep_add. Forgejo + GitHub."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithNumber("blocker"),
		mcp.WithNumber("blocks"),
	), ctxBoundHandler(handleIssueDepRemove))
}

func handleIssueDepList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	n := optInt(args, "number")
	if n <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "number is required")), nil
	}
	direction := optString(args, "direction")
	if direction == "" {
		direction = "blockers"
	}
	if direction != "blockers" && direction != "blocks" {
		return toolError(exitcode.Errorf(exitcode.Usage,
			`direction must be "blockers" or "blocks", got %q`, direction)), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	po := provider.ListIssueDepsOptions{
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	}
	var issues []types.Issue
	var page *provider.Page
	switch direction {
	case "blockers":
		issues, page, err = p.ListIssueDependencies(ctx, owner, repo, n, po)
	case "blocks":
		issues, page, err = p.ListIssueBlocks(ctx, owner, repo, n, po)
	}
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(issues, page), nil
}

func handleIssueDepAdd(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	target, host, err := mcpResolveDepDirection(args)
	if err != nil {
		return toolError(err), nil
	}
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	added, err := p.AddIssueDependency(ctx, owner, repo, host, target)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(added, nil), nil
}

func handleIssueDepRemove(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	target, host, err := mcpResolveDepDirection(args)
	if err != nil {
		return toolError(err), nil
	}
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.RemoveIssueDependency(ctx, owner, repo, host, target); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{
		"removed_edge_from": host,
		"removed_edge_to":   target,
	}, nil), nil
}

// mcpResolveDepDirection enforces the mutual exclusion of blocker /
// blocks args and returns (target, host) such that
// AddIssueDependency(host, target) creates the edge "target blocks
// host." Mirrors resolveDepDirection in internal/cli/issue_dep.go;
// reads the `number` arg as the issue the edge is anchored to.
//
// Exactly one of blocker / blocks must be > 0.
func mcpResolveDepDirection(args map[string]any) (target, host int, err error) {
	n := optInt(args, "number")
	if n <= 0 {
		return 0, 0, exitcode.Errorf(exitcode.Usage, "number is required (positive issue number)")
	}
	blocker := optInt(args, "blocker")
	blocks := optInt(args, "blocks")
	switch {
	case blocker > 0 && blocks > 0:
		return 0, 0, exitcode.Errorf(exitcode.Usage,
			"blocker and blocks are mutually exclusive")
	case blocker > 0:
		return blocker, n, nil
	case blocks > 0:
		return n, blocks, nil
	default:
		return 0, 0, exitcode.Errorf(exitcode.Usage,
			"one of blocker or blocks is required (positive issue number)")
	}
}
