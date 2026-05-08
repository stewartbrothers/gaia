package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerActionsTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_actions_list_runs",
		mcp.WithDescription("List recent workflow runs for a repo (newest first). Trimmed WorkflowRun records inside the gaia envelope. Filter by status or branch."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("status", mcp.Description("filter by status: waiting, running, success, failure, cancelled")),
		mcp.WithString("branch", mcp.Description("filter by branch name")),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleActionsListRuns))

	s.AddTool(mcp.NewTool("gaia_actions_view_run",
		mcp.WithDescription("Get one workflow run by ID with its jobs and steps inlined."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("run_id", mcp.Required()),
	), ctxBoundHandler(handleActionsViewRun))

	s.AddTool(mcp.NewTool("gaia_actions_get_logs",
		mcp.WithDescription("Fetch per-job log lines for a workflow run. Defaults to failed-only so agents get the relevant output first. Set failed_only=false to retrieve all logs."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("run_id", mcp.Required()),
		mcp.WithBoolean("failed_only", mcp.Description("return only logs from failed jobs (default true)")),
	), ctxBoundHandler(handleActionsGetLogs))

	s.AddTool(mcp.NewTool("gaia_actions_rerun",
		mcp.WithDescription("Re-trigger a workflow run by ID."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("run_id", mcp.Required()),
	), ctxBoundHandler(handleActionsRerun))
}

func handleActionsListRuns(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	runs, page, err := p.ListWorkflowRuns(ctx, owner, repo, provider.ListWorkflowRunsOptions{
		Status: optString(args, "status"),
		Branch: optString(args, "branch"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(runs, page), nil
}

func handleActionsViewRun(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	runID := optInt64(args, "run_id")
	if runID <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "run_id is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	run, err := p.GetWorkflowRun(ctx, owner, repo, runID, provider.GetWorkflowRunOptions{WithJobs: true})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(run, nil), nil
}

func handleActionsGetLogs(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	runID := optInt64(args, "run_id")
	if runID <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "run_id is required")), nil
	}

	// Default to failed-only for agent use; the caller can opt out.
	failedOnly := true
	if v, present := args["failed_only"]; present {
		if b, ok := v.(bool); ok {
			failedOnly = b
		}
	}

	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	logs, err := p.GetWorkflowRunLogs(ctx, owner, repo, runID, provider.GetWorkflowRunLogsOptions{
		FailedOnly: failedOnly,
	})
	if err != nil {
		return toolError(err), nil
	}

	// Return the logs as a plain text block — easier to read in a chat
	// interface than a JSON envelope, and token-cheaper than JSON with
	// escape sequences per line. Agents that need structured access can
	// use the CLI with --format json.
	if len(logs) == 0 {
		return mcp.NewToolResultText("(no logs)"), nil
	}
	var sb strings.Builder
	for _, jl := range logs {
		fmt.Fprintf(&sb, "=== Job %d: %s ===\n", jl.JobID, jl.JobName)
		for _, line := range jl.Lines {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func handleActionsRerun(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	runID := optInt64(args, "run_id")
	if runID <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "run_id is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.RerunWorkflowRun(ctx, owner, repo, runID); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"rerun": runID}, nil), nil
}
