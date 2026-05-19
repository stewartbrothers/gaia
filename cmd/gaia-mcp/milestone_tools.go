package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerMilestoneTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_milestone_list",
		mcp.WithDescription("List milestones on a repo. Trimmed Milestone records inside the gaia envelope."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("state", mcp.Description("open (default), closed, all")),
		mcp.WithString("name", mcp.Description("title substring filter (Forgejo only)")),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleMilestoneList))

	s.AddTool(mcp.NewTool("gaia_milestone_view",
		mcp.WithDescription("Get one milestone by ID."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("milestone ID")),
	), ctxBoundHandler(handleMilestoneView))

	s.AddTool(mcp.NewTool("gaia_milestone_create",
		mcp.WithDescription("Create a new milestone. due_on is an RFC3339 timestamp; omit for no deadline."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("title", mcp.Required()),
		mcp.WithString("description"),
		mcp.WithString("due_on", mcp.Description("RFC3339 timestamp, e.g. 2026-06-01T00:00:00Z")),
	), ctxBoundHandler(handleMilestoneCreate))

	s.AddTool(mcp.NewTool("gaia_milestone_edit",
		mcp.WithDescription("Edit a milestone by ID. Empty fields are unchanged. state takes open or closed."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
		mcp.WithString("title"),
		mcp.WithString("description"),
		mcp.WithString("state", mcp.Description("open or closed")),
		mcp.WithString("due_on", mcp.Description("RFC3339 timestamp")),
	), ctxBoundHandler(handleMilestoneEdit))

	s.AddTool(mcp.NewTool("gaia_milestone_delete",
		mcp.WithDescription("Delete a milestone by ID. confirm=true required to actually remove (preview otherwise)."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
		mcp.WithBoolean("confirm"),
	), ctxBoundHandler(handleMilestoneDelete))

	s.AddTool(mcp.NewTool("gaia_milestone_issues",
		mcp.WithDescription("List issues attached to a milestone."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
		mcp.WithString("state", mcp.Description("open (default), closed, all")),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleMilestoneIssues))
}

func handleMilestoneList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	ms, page, err := p.ListMilestones(ctx, owner, repo, provider.ListMilestonesOptions{
		State:  optString(args, "state"),
		Name:   optString(args, "name"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(ms, page), nil
}

func handleMilestoneView(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id, err := requireMilestoneID(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	m, err := p.GetMilestone(ctx, owner, repo, id)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(m, nil), nil
}

func handleMilestoneCreate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	title := optString(args, "title")
	if title == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "title is required")), nil
	}
	opts := provider.CreateMilestoneOptions{
		Title:       title,
		Description: optString(args, "description"),
	}
	if due := optString(args, "due_on"); due != "" {
		t, err := time.Parse(time.RFC3339, due)
		if err != nil {
			return toolError(exitcode.Errorf(exitcode.Usage,
				"due_on must be RFC3339; got %q", due)), nil
		}
		opts.DueOn = &t
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	m, err := p.CreateMilestone(ctx, owner, repo, opts)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(m, nil), nil
}

func handleMilestoneEdit(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id, err := requireMilestoneID(args)
	if err != nil {
		return toolError(err), nil
	}
	opts := provider.EditMilestoneOptions{
		Title:       optString(args, "title"),
		Description: optString(args, "description"),
		State:       optString(args, "state"),
	}
	if due := optString(args, "due_on"); due != "" {
		t, err := time.Parse(time.RFC3339, due)
		if err != nil {
			return toolError(exitcode.Errorf(exitcode.Usage,
				"due_on must be RFC3339; got %q", due)), nil
		}
		opts.DueOn = &t
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	m, err := p.EditMilestone(ctx, owner, repo, id, opts)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(m, nil), nil
}

func handleMilestoneDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id, err := requireMilestoneID(args)
	if err != nil {
		return toolError(err), nil
	}
	if !optBool(args, "confirm") {
		return toolResult(map[string]any{
			"would_delete": id,
			"confirmed":    false,
		}, nil), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.DeleteMilestone(ctx, owner, repo, id); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"deleted": id}, nil), nil
}

func handleMilestoneIssues(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id, err := requireMilestoneID(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	issues, page, err := p.ListMilestoneIssues(ctx, owner, repo, id, provider.ListMilestoneIssuesOptions{
		State:  optString(args, "state"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(issues, page), nil
}

// requireMilestoneID extracts the `id` argument as int64. JSON
// numbers come through as float64; coerce. Zero or negative is a
// usage error.
func requireMilestoneID(args map[string]any) (int64, error) {
	v, present := args["id"]
	if !present {
		return 0, exitcode.Errorf(exitcode.Usage, "id is required")
	}
	switch n := v.(type) {
	case float64:
		if n <= 0 {
			return 0, exitcode.Errorf(exitcode.Usage, "id must be positive; got %v", n)
		}
		return int64(n), nil
	case int:
		if n <= 0 {
			return 0, exitcode.Errorf(exitcode.Usage, "id must be positive; got %d", n)
		}
		return int64(n), nil
	case int64:
		if n <= 0 {
			return 0, exitcode.Errorf(exitcode.Usage, "id must be positive; got %d", n)
		}
		return n, nil
	default:
		return 0, exitcode.Errorf(exitcode.Usage, "id must be a number; got %s", fmt.Sprintf("%T", v))
	}
}
