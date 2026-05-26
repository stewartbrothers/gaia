package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
		mcp.WithDescription(`Add a dependency edge. Two framings (mutually exclusive): blocker=M means "M blocks N" (where N is the `+"`number`"+` arg); blocks=M means "N blocks M". Same edge, opposite direction of argument flow. Cross-repo (#325) via blocker_repo / blocks_repo args, or by passing blocker/blocks as the string "owner/repo#M". Forgejo + GitHub.`),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required(), mcp.Description("the issue the edge is anchored to")),
		mcp.WithNumber("blocker", mcp.Description(`M where "M blocks N" (number for same-repo; pair with blocker_repo for cross-repo, or pass "owner/repo#M" as a string)`)),
		mcp.WithString("blocker_repo", mcp.Description(`owner/repo for a cross-repo blocker (#325). Optional pair with numeric blocker.`)),
		mcp.WithNumber("blocks", mcp.Description(`M where "N blocks M" (number for same-repo; pair with blocks_repo for cross-repo, or pass "owner/repo#M" as a string)`)),
		mcp.WithString("blocks_repo", mcp.Description(`owner/repo for a cross-repo target. Optional pair with numeric blocks.`)),
	), ctxBoundHandler(handleIssueDepAdd))

	s.AddTool(mcp.NewTool("gaia_issue_dep_remove",
		mcp.WithDescription("Remove a dependency edge. Same blocker/blocks framing + cross-repo support as gaia_issue_dep_add. Forgejo + GitHub."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithNumber("blocker"),
		mcp.WithString("blocker_repo"),
		mcp.WithNumber("blocks"),
		mcp.WithString("blocks_repo"),
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
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	host, target, err := mcpResolveDepDirection(owner, repo, args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	added, err := p.AddIssueDependency(ctx, host.Owner, host.Repo, host.Number, target)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(added, nil), nil
}

func handleIssueDepRemove(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	host, target, err := mcpResolveDepDirection(owner, repo, args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.RemoveIssueDependency(ctx, host.Owner, host.Repo, host.Number, target); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{
		"removed_edge_from": fmt.Sprintf("%s/%s#%d", host.Owner, host.Repo, host.Number),
		"removed_edge_to":   fmt.Sprintf("%s/%s#%d", defaultIfEmpty(target.Owner, host.Owner), defaultIfEmpty(target.Repo, host.Repo), target.Number),
	}, nil), nil
}

// mcpDepAnchor mirrors the CLI's depAnchor — the host or target side
// of a dependency edge, with owner+repo populated explicitly so
// cross-repo refs (#325) carry their location.
type mcpDepAnchor struct {
	Owner  string
	Repo   string
	Number int
}

// mcpResolveDepDirection enforces the mutual exclusion of blocker /
// blocks args and returns (host, target) anchors. Mirrors
// resolveDepDirection in internal/cli/issue_dep.go. blocker/blocks
// args may be either:
//   - a bare integer JSON number (same-repo)
//   - the string form "owner/repo#N" (cross-repo, #325)
//
// blocker_repo / blocks_repo string args also accepted alongside
// the numeric form for explicit cross-repo: passing blocker=7 +
// blocker_repo="owner/repo" is equivalent to blocker="owner/repo#7".
// Provided as a typed alternative since MCP tools often prefer
// explicit numeric args.
func mcpResolveDepDirection(hostOwner, hostRepo string, args map[string]any) (host mcpDepAnchor, target provider.IssueDepRef, err error) {
	n := optInt(args, "number")
	if n <= 0 {
		return mcpDepAnchor{}, provider.IssueDepRef{}, exitcode.Errorf(exitcode.Usage,
			"number is required (positive issue number)")
	}
	blocker, blockerRepo := optMixedRef(args, "blocker", "blocker_repo")
	blocks, blocksRepo := optMixedRef(args, "blocks", "blocks_repo")
	switch {
	case blocker.Number > 0 && blocks.Number > 0:
		return mcpDepAnchor{}, provider.IssueDepRef{}, exitcode.Errorf(exitcode.Usage,
			"blocker and blocks are mutually exclusive")
	case blocker.Number > 0:
		_ = blockerRepo // captured into blocker via optMixedRef
		host = mcpDepAnchor{Owner: hostOwner, Repo: hostRepo, Number: n}
		target = provider.IssueDepRef{Owner: blocker.Owner, Repo: blocker.Repo, Number: blocker.Number}
		return host, target, nil
	case blocks.Number > 0:
		_ = blocksRepo
		if blocks.Owner == "" {
			host = mcpDepAnchor{Owner: hostOwner, Repo: hostRepo, Number: blocks.Number}
			target = provider.IssueDepRef{Number: n}
		} else {
			host = mcpDepAnchor{Owner: blocks.Owner, Repo: blocks.Repo, Number: blocks.Number}
			target = provider.IssueDepRef{Owner: hostOwner, Repo: hostRepo, Number: n}
		}
		return host, target, nil
	default:
		return mcpDepAnchor{}, provider.IssueDepRef{}, exitcode.Errorf(exitcode.Usage,
			"one of blocker or blocks is required (positive issue number)")
	}
}

// optMixedRef reads a "blocker"/"blocks"-style arg that can be
// either: a JSON number (same-repo, paired with optional
// blocker_repo / blocks_repo string), or a string in
// "owner/repo#N" form (cross-repo, inline). Returns a numeric-only
// anchor + the explicit repo string (caller threads them into the
// final anchor).
func optMixedRef(args map[string]any, numKey, repoKey string) (anchor mcpDepAnchor, repoFlag string) {
	repoFlag = optString(args, repoKey)
	switch v := args[numKey].(type) {
	case float64, int:
		// Numeric form. Pair with explicit repoFlag if present.
		n := optInt(args, numKey)
		anchor.Number = n
		if repoFlag != "" {
			if o, r, ok := splitOwnerRepo(repoFlag); ok {
				anchor.Owner = o
				anchor.Repo = r
			}
		}
	case string:
		// String form — either "7" or "owner/repo#7".
		if v == "" {
			return mcpDepAnchor{}, repoFlag
		}
		// Reuse the CLI's parse helper via the exported test re-
		// export? No — that lives in internal/cli and we can't
		// import it here. Re-implement the substring match
		// inline; it's tiny.
		if i := strings.Index(v, "#"); i > 0 {
			ownerRepo := v[:i]
			numStr := v[i+1:]
			if o, r, ok := splitOwnerRepo(ownerRepo); ok {
				n, perr := strconv.Atoi(numStr)
				if perr == nil && n > 0 {
					anchor.Owner = o
					anchor.Repo = r
					anchor.Number = n
				}
			}
		} else {
			n, perr := strconv.Atoi(v)
			if perr == nil && n > 0 {
				anchor.Number = n
			}
		}
	}
	return anchor, repoFlag
}

// splitOwnerRepo splits "owner/repo" → ("owner", "repo", true).
// Returns false on malformed input.
func splitOwnerRepo(s string) (owner, repo string, ok bool) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
