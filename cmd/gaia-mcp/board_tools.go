package main

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// Kanban boards and the Do Now flag are Aikestra-only (#404); the tools
// register only when the provider declares CapBoards / CapIssuePriority.

func registerBoardTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_board_list",
		mcp.WithDescription("List the project's kanban boards and their stages (Aikestra). Boards belong to the owner; the repo half of `repo` is ignored."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name (owner = Aikestra Project slug)")),
		mcp.WithNumber("limit", mcp.Description("max boards to return (cap 100)")),
		mcp.WithString("cursor", mcp.Description("opaque pagination cursor from a previous response")),
	), ctxBoundHandler(handleBoardList))

	s.AddTool(mcp.NewTool("gaia_board_view",
		mcp.WithDescription("Show one board's stages with their open issue numbers in queue order (Aikestra)."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name (owner = Aikestra Project slug)")),
		mcp.WithString("board", mcp.Required(), mcp.Description("board name (case-insensitive) or ID")),
	), ctxBoundHandler(handleBoardView))

	s.AddTool(mcp.NewTool("gaia_issue_move",
		mcp.WithDescription("Move an issue to another board stage (Aikestra). Refused with exit 9 (PolicyViolation) while an open issue blocks it; the error names the blockers."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("number", mcp.Required(), mcp.Description("issue number")),
		mcp.WithString("stage", mcp.Required(), mcp.Description("target stage name (case-insensitive) or ID")),
		mcp.WithString("board", mcp.Description("target board name or ID; omit to stay on the issue's current board")),
		mcp.WithString("note", mcp.Description("why, recorded with the move (required when the Project requires notes)")),
	), ctxBoundHandler(handleIssueMove))
}

func registerIssuePriorityTool(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_issue_priority",
		mcp.WithDescription("Set or clear an issue's Do Now priority flag (Aikestra)."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("number", mcp.Required(), mcp.Description("issue number")),
		mcp.WithBoolean("do_now", mcp.Required(), mcp.Description("true = Do Now, false = normal priority")),
	), ctxBoundHandler(handleIssuePriority))
}

func handleBoardList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, _, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	boards, page, err := p.ListBoards(ctx, owner, provider.ListBoardsOptions{
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(boards, page), nil
}

func handleBoardView(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, _, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	name := strings.TrimSpace(optString(args, "board"))
	if name == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "board is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	board, err := p.GetBoard(ctx, owner, name)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(board, nil), nil
}

func handleIssueMove(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	n := optInt(args, "number")
	if n <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "number is required")), nil
	}
	opts := provider.MoveIssueOptions{
		Stage: strings.TrimSpace(optString(args, "stage")),
		Board: optString(args, "board"),
		Note:  optString(args, "note"),
	}
	if opts.Stage == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "stage is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	issue, err := p.MoveIssue(ctx, owner, repo, n, opts)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(issue, nil), nil
}

func handleIssuePriority(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	n := optInt(args, "number")
	if n <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "number is required")), nil
	}
	if _, ok := args["do_now"].(bool); !ok {
		return toolError(exitcode.Errorf(exitcode.Usage, "do_now is required (true or false)")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	issue, err := p.SetIssuePriority(ctx, owner, repo, n, optBool(args, "do_now"))
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(issue, nil), nil
}
