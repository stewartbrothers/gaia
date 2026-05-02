package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerIssueTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_issue_list",
		mcp.WithDescription("List issues in a repo. Returns trimmed Issue records inside the gaia envelope."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("state", mcp.Description("open | closed | all")),
		mcp.WithArray("labels", mcp.Description("filter to issues with these label names"), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithString("query", mcp.Description("substring search")),
		mcp.WithNumber("limit", mcp.Description("max results (default 30, cap 200)")),
		mcp.WithString("cursor", mcp.Description("opaque pagination cursor from a previous response")),
	), ctxBoundHandler(handleIssueList))

	s.AddTool(mcp.NewTool("gaia_issue_view",
		mcp.WithDescription("Get one issue. Optionally inlines recent comments."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("number", mcp.Required(), mcp.Description("issue number")),
		mcp.WithNumber("with_comments", mcp.Description("inline this many recent comments (0 = none)")),
	), ctxBoundHandler(handleIssueView))

	s.AddTool(mcp.NewTool("gaia_issue_create",
		mcp.WithDescription("Open a new issue."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("title", mcp.Required(), mcp.Description("issue title")),
		mcp.WithString("body", mcp.Description("issue body (markdown)")),
		mcp.WithArray("labels", mcp.Description("label names"), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithArray("assignees", mcp.Description("assignee logins"), mcp.Items(map[string]any{"type": "string"})),
	), ctxBoundHandler(handleIssueCreate))

	s.AddTool(mcp.NewTool("gaia_issue_edit",
		mcp.WithDescription("Edit an issue (title/body/state/assignees). Empty fields are unchanged."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithString("title"),
		mcp.WithString("body"),
		mcp.WithString("state", mcp.Description("open | closed")),
		mcp.WithArray("assignees", mcp.Description("replace assignees with these logins"), mcp.Items(map[string]any{"type": "string"})),
	), ctxBoundHandler(handleIssueEdit))

	s.AddTool(mcp.NewTool("gaia_issue_comment",
		mcp.WithDescription("Post a top-level thread comment on an issue or PR."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithString("body", mcp.Required(), mcp.Description("comment body (markdown)")),
	), ctxBoundHandler(handleIssueComment))
}

func handleIssueList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	issues, page, err := p.ListIssues(ctx, owner, repo, provider.ListIssuesOptions{
		State:  optString(args, "state"),
		Labels: optStringSlice(args, "labels"),
		Query:  optString(args, "query"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(issues, page), nil
}

func handleIssueView(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	n := optInt(args, "number")
	if n <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "number is required")), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	issue, err := p.GetIssue(ctx, owner, repo, n, provider.GetIssueOptions{
		WithComments: optInt(args, "with_comments"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(issue, nil), nil
}

func handleIssueCreate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	title := optString(args, "title")
	if title == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "title is required")), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	issue, err := p.CreateIssue(ctx, owner, repo, provider.CreateIssueOptions{
		Title:     title,
		Body:      optString(args, "body"),
		Labels:    optStringSlice(args, "labels"),
		Assignees: optStringSlice(args, "assignees"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(issue, nil), nil
}

func handleIssueEdit(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	n := optInt(args, "number")
	if n <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "number is required")), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	issue, err := p.EditIssue(ctx, owner, repo, n, provider.EditIssueOptions{
		Title:     optString(args, "title"),
		Body:      optString(args, "body"),
		State:     optString(args, "state"),
		Assignees: optStringSlice(args, "assignees"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(issue, nil), nil
}

func handleIssueComment(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	n := optInt(args, "number")
	body := optString(args, "body")
	if n <= 0 || body == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "number and body are required")), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	c, err := p.CreateIssueComment(ctx, owner, repo, n, body)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(c, nil), nil
}

// reference fmt to keep the import live for future error formatting.
var _ = fmt.Sprintf
