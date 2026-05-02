package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerPRTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_pr_list",
		mcp.WithDescription("List pull requests."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("state", mcp.Description("open | closed | all")),
		mcp.WithArray("labels", mcp.Items(map[string]any{"type": "string"})),
		mcp.WithString("head"),
		mcp.WithString("base"),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handlePRList))

	s.AddTool(mcp.NewTool("gaia_pr_view",
		mcp.WithDescription("Get one PR. Optionally fetches CI summary and inlines recent thread comments."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithBoolean("with_ci", mcp.Description("fetch CI status for the head commit")),
		mcp.WithNumber("with_comments", mcp.Description("inline this many recent thread comments")),
	), ctxBoundHandler(handlePRView))

	s.AddTool(mcp.NewTool("gaia_pr_diff",
		mcp.WithDescription("Get the structured diff for a PR (parsed unified-diff). Use --fields path,status to keep output small."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithArray("paths", mcp.Description("filter to specific file paths"), mcp.Items(map[string]any{"type": "string"})),
	), ctxBoundHandler(handlePRDiff))

	s.AddTool(mcp.NewTool("gaia_pr_comments",
		mcp.WithDescription("Get the unified comment stream (issue + review + inline) for a PR."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithArray("sources", mcp.Description("filter by source: issue, review, inline"), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithNumber("limit"),
	), ctxBoundHandler(handlePRComments))

	s.AddTool(mcp.NewTool("gaia_pr_create",
		mcp.WithDescription("Open a new pull request."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("title", mcp.Required()),
		mcp.WithString("head", mcp.Required(), mcp.Description("head ref (e.g. feature/x or owner:ref for forks)")),
		mcp.WithString("base", mcp.Required(), mcp.Description("base ref (e.g. main)")),
		mcp.WithString("body"),
		mcp.WithBoolean("draft"),
		mcp.WithArray("labels", mcp.Items(map[string]any{"type": "string"})),
	), ctxBoundHandler(handlePRCreate))

	s.AddTool(mcp.NewTool("gaia_pr_edit",
		mcp.WithDescription("Edit a PR (title/body/state/draft). Empty fields are unchanged."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithString("title"),
		mcp.WithString("body"),
		mcp.WithString("state", mcp.Description("open | closed")),
		mcp.WithBoolean("draft", mcp.Description("set draft state explicitly (omit to leave unchanged)")),
	), ctxBoundHandler(handlePREdit))

	s.AddTool(mcp.NewTool("gaia_pr_merge",
		mcp.WithDescription("Merge a pull request."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithString("method", mcp.Description("merge | rebase | squash (default merge)")),
		mcp.WithString("title"),
		mcp.WithString("message"),
		mcp.WithBoolean("delete_branch"),
	), ctxBoundHandler(handlePRMerge))

	s.AddTool(mcp.NewTool("gaia_pr_review",
		mcp.WithDescription("Submit a PR review. State maps to event (APPROVED/REQUEST_CHANGES/COMMENT). Inline comments are an array of {path, line, body}."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("number", mcp.Required()),
		mcp.WithString("state", mcp.Required(), mcp.Description("approve | request-changes | comment")),
		mcp.WithString("body"),
		mcp.WithArray("comments",
			mcp.Description("inline review comments"),
			mcp.Items(map[string]any{
				"type":     "object",
				"required": []string{"path", "line", "body"},
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
					"line": map[string]any{"type": "number"},
					"body": map[string]any{"type": "string"},
				},
			}),
		),
	), ctxBoundHandler(handlePRReview))
}

func handlePRList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	prs, page, err := p.ListPullRequests(ctx, owner, repo, provider.ListPullRequestsOptions{
		State:  optString(args, "state"),
		Labels: optStringSlice(args, "labels"),
		Head:   optString(args, "head"),
		Base:   optString(args, "base"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(prs, page), nil
}

func handlePRView(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	pr, err := p.GetPullRequest(ctx, owner, repo, n, provider.GetPullRequestOptions{
		WithCISummary: optBool(args, "with_ci"),
		WithComments:  optInt(args, "with_comments"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(pr, nil), nil
}

func handlePRDiff(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	files, err := p.GetPullRequestDiff(ctx, owner, repo, n, provider.GetPullRequestDiffOptions{
		Paths: optStringSlice(args, "paths"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(files, nil), nil
}

func handlePRComments(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	cs, err := p.ListComments(ctx, owner, repo, n, provider.ListCommentsOptions{
		Sources: optStringSlice(args, "sources"),
		Limit:   optInt(args, "limit"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(cs, nil), nil
}

func handlePRCreate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	title := optString(args, "title")
	head := optString(args, "head")
	base := optString(args, "base")
	if title == "" || head == "" || base == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "title, head, and base are required")), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	pr, err := p.CreatePullRequest(ctx, owner, repo, provider.CreatePullRequestOptions{
		Title:  title,
		Body:   optString(args, "body"),
		Head:   head,
		Base:   base,
		Draft:  optBool(args, "draft"),
		Labels: optStringSlice(args, "labels"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(pr, nil), nil
}

func handlePREdit(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	n := optInt(args, "number")
	if n <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "number is required")), nil
	}
	opts := provider.EditPullRequestOptions{
		Title: optString(args, "title"),
		Body:  optString(args, "body"),
		State: optString(args, "state"),
	}
	if v, present := args["draft"]; present {
		if b, ok := v.(bool); ok {
			opts.Draft = &b
		}
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	pr, err := p.EditPullRequest(ctx, owner, repo, n, opts)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(pr, nil), nil
}

func handlePRMerge(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	if err := p.MergePullRequest(ctx, owner, repo, n, provider.MergePullRequestOptions{
		Method:       optString(args, "method"),
		Title:        optString(args, "title"),
		Message:      optString(args, "message"),
		DeleteBranch: optBool(args, "delete_branch"),
	}); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"merged": true, "number": n}, nil), nil
}

func handlePRReview(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	n := optInt(args, "number")
	if n <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "number is required")), nil
	}
	event, err := stateToEvent(optString(args, "state"))
	if err != nil {
		return toolError(err), nil
	}
	inline, err := parseInlineFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build()
	if err != nil {
		return toolError(err), nil
	}
	if err := p.SubmitReview(ctx, owner, repo, n, provider.SubmitReviewOptions{
		Event:    event,
		Body:     optString(args, "body"),
		Comments: inline,
	}); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"submitted": true, "number": n, "event": event}, nil), nil
}

func stateToEvent(state string) (string, error) {
	switch state {
	case "approve", "approved", "APPROVED":
		return "APPROVED", nil
	case "request-changes", "request_changes", "REQUEST_CHANGES", "changes":
		return "REQUEST_CHANGES", nil
	case "comment", "COMMENT", "":
		return "COMMENT", nil
	default:
		return "", exitcode.Errorf(exitcode.Usage, "state must be approve|request-changes|comment; got %q", state)
	}
}

// parseInlineFromArgs reads args["comments"] (an array of objects)
// into ReviewInlineComment values. Each object must carry path, line,
// body.
func parseInlineFromArgs(args map[string]any) ([]provider.ReviewInlineComment, error) {
	raw, ok := args["comments"].([]any)
	if !ok {
		return nil, nil
	}
	out := make([]provider.ReviewInlineComment, 0, len(raw))
	for i, x := range raw {
		obj, ok := x.(map[string]any)
		if !ok {
			return nil, exitcode.Errorf(exitcode.Usage, "comments[%d] must be an object", i)
		}
		path, _ := obj["path"].(string)
		body, _ := obj["body"].(string)
		var line int
		switch v := obj["line"].(type) {
		case float64:
			line = int(v)
		case int:
			line = v
		}
		if path == "" || body == "" || line < 1 {
			return nil, exitcode.Errorf(exitcode.Usage, "comments[%d] needs non-empty path, body, and positive line", i)
		}
		out = append(out, provider.ReviewInlineComment{Path: path, Line: line, Body: body})
	}
	return out, nil
}
