// Package forgejo: Actions workflow run inspection (#183).
//
// Forgejo Actions endpoints used here:
//
//	GET  /repos/{o}/{r}/actions/runs                        — list runs
//	GET  /repos/{o}/{r}/actions/runs/{run_id}               — single run
//	GET  /repos/{o}/{r}/actions/tasks?run_id={id}&limit=50  — list tasks (jobs)
//	GET  /repos/{o}/{r}/actions/tasks/{task_id}/logs        — ZIP of step logs
//	POST /repos/{o}/{r}/actions/runs/{run_id}/rerun         — re-trigger
//
// The Forgejo API reuses the key "workflow_runs" for both workflow runs
// and tasks (the latter are per-job units). Both response envelopes look
// the same at the top level but have different element shapes.
package forgejo

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// --- Wire types --------------------------------------------------------

// apiWorkflowRun mirrors the Forgejo workflow-run record. Only fields
// gaia trims into WorkflowRun are decoded; everything else is dropped.
type apiWorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadBranch string `json:"head_branch"`
	HeadSHA    string `json:"head_sha"`
	HeadCommit struct {
		Message string `json:"message"`
	} `json:"head_commit"`
	TriggerActor struct {
		Login string `json:"login"`
	} `json:"trigger_actor"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type apiWorkflowRunList struct {
	TotalCount   int              `json:"total_count"`
	WorkflowRuns []apiWorkflowRun `json:"workflow_runs"`
}

// parseRunTime parses a Forgejo RFC3339 timestamp; returns zero on failure.
func parseRunTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (a *apiWorkflowRun) toRun() types.WorkflowRun {
	return types.WorkflowRun{
		ID:           a.ID,
		WorkflowName: a.Name,
		Event:        a.Event,
		Status:       a.Status,
		Conclusion:   a.Conclusion,
		Branch:       a.HeadBranch,
		HeadSHA:      a.HeadSHA,
		HeadMessage:  a.HeadCommit.Message,
		Actor:        types.User{Login: a.TriggerActor.Login},
		CreatedAt:    parseRunTime(a.CreatedAt),
		UpdatedAt:    parseRunTime(a.UpdatedAt),
	}
}

// apiWorkflowTask is Forgejo's "task" — one job within a run.
// The API endpoint is /actions/tasks and the envelope key is still
// "workflow_runs" (Forgejo's naming is historic).
type apiWorkflowTask struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	StartedAt  string `json:"started_at"`
	StoppedAt  string `json:"stopped_at"`
	Steps      []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Number     int    `json:"number"`
	} `json:"steps"`
}

type apiTaskList struct {
	TotalCount   int               `json:"total_count"`
	WorkflowRuns []apiWorkflowTask `json:"workflow_runs"`
}

func (a *apiWorkflowTask) toJob() types.WorkflowJob {
	job := types.WorkflowJob{
		ID:         a.ID,
		Name:       a.Name,
		Status:     a.Status,
		Conclusion: a.Conclusion,
	}
	if t := parseRunTime(a.StartedAt); !t.IsZero() {
		job.StartedAt = &t
	}
	if t := parseRunTime(a.StoppedAt); !t.IsZero() {
		job.CompletedAt = &t
	}
	for _, s := range a.Steps {
		job.Steps = append(job.Steps, types.WorkflowStep{
			Name:       s.Name,
			Status:     s.Status,
			Conclusion: s.Conclusion,
			Number:     s.Number,
		})
	}
	return job
}

// --- Provider methods --------------------------------------------------

// ListWorkflowRuns returns recent workflow runs for the repo, newest first.
func (p *Provider) ListWorkflowRuns(ctx context.Context, owner, repo string, opts provider.ListWorkflowRunsOptions) ([]types.WorkflowRun, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Branch != "" {
		q.Set("branch", opts.Branch)
	}

	path := fmt.Sprintf("/repos/%s/%s/actions/runs?%s", owner, repo, q.Encode())
	var raw apiWorkflowRunList
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.WorkflowRun, 0, len(raw.WorkflowRuns))
	for i := range raw.WorkflowRuns {
		out = append(out, raw.WorkflowRuns[i].toRun())
	}
	return out, makePage(len(raw.WorkflowRuns), limit, opts.Cursor), nil
}

// GetWorkflowRun fetches one run by ID. When opts.WithJobs is true a
// second request lists the run's tasks (jobs) and inlines them.
func (p *Provider) GetWorkflowRun(ctx context.Context, owner, repo string, runID int64, opts provider.GetWorkflowRunOptions) (*types.WorkflowRun, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", owner, repo, runID)
	var raw apiWorkflowRun
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	run := raw.toRun()

	if opts.WithJobs {
		jobs, err := p.listTasks(ctx, owner, repo, runID)
		if err != nil {
			return nil, err
		}
		run.Jobs = jobs
	}
	return &run, nil
}

// listTasks fetches up to 50 tasks for a run from the tasks endpoint.
func (p *Provider) listTasks(ctx context.Context, owner, repo string, runID int64) ([]types.WorkflowJob, error) {
	q := url.Values{}
	q.Set("run_id", strconv.FormatInt(runID, 10))
	q.Set("limit", "50")

	path := fmt.Sprintf("/repos/%s/%s/actions/tasks?%s", owner, repo, q.Encode())
	var raw apiTaskList
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]types.WorkflowJob, 0, len(raw.WorkflowRuns))
	for i := range raw.WorkflowRuns {
		out = append(out, raw.WorkflowRuns[i].toJob())
	}
	return out, nil
}

// GetWorkflowRunLogs fetches per-job log lines for a run. Forgejo
// returns a ZIP per task; this method decodes each ZIP and returns
// the lines per job. When opts.FailedOnly is true only jobs whose
// Conclusion is "failure" have their logs fetched.
func (p *Provider) GetWorkflowRunLogs(ctx context.Context, owner, repo string, runID int64, opts provider.GetWorkflowRunLogsOptions) ([]types.WorkflowRunLogs, error) {
	// First get the task list so we know the job IDs and names.
	tasks, err := p.listTasks(ctx, owner, repo, runID)
	if err != nil {
		return nil, err
	}

	var out []types.WorkflowRunLogs
	for _, job := range tasks {
		if opts.FailedOnly && job.Conclusion != "failure" {
			continue
		}
		lines, err := p.fetchTaskLogs(ctx, owner, repo, job.ID)
		if err != nil {
			// Non-fatal: log fetch errors are surfaced as an empty entry
			// rather than aborting the whole call. The job name indicates
			// which one failed.
			out = append(out, types.WorkflowRunLogs{
				JobID:   job.ID,
				JobName: job.Name,
				Lines:   []string{fmt.Sprintf("(log fetch error: %v)", err)},
			})
			continue
		}
		out = append(out, types.WorkflowRunLogs{
			JobID:   job.ID,
			JobName: job.Name,
			Lines:   lines,
		})
	}
	return out, nil
}

// fetchTaskLogs downloads the ZIP from the task-logs endpoint and
// returns all log lines across all step files in the archive. Each
// ZIP entry is one step's log. Lines are concatenated in entry order
// (Forgejo names entries with a numeric prefix so order is stable).
func (p *Provider) fetchTaskLogs(ctx context.Context, owner, repo string, taskID int64) ([]string, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/tasks/%d/logs", owner, repo, taskID)
	raw, err := p.client.GetRaw(ctx, path)
	if err != nil {
		return nil, err
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("forgejo: decode task log ZIP for task %d: %w", taskID, err)
	}

	var lines []string
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		_ = rc.Close()
	}
	return lines, nil
}

// RerunWorkflowRun re-triggers a workflow run. Forgejo returns 204.
func (p *Provider) RerunWorkflowRun(ctx context.Context, owner, repo string, runID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/rerun", owner, repo, runID)
	return p.client.Post(ctx, path, nil, nil)
}
