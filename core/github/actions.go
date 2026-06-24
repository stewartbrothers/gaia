// Package github: Actions workflow run inspection (#386).
//
// Implements the four core.Provider Actions methods against the GitHub
// REST API. Unlike the Forgejo provider — where logs and rerun are
// unavailable because the v15 API exposes no endpoint (#266, #267) —
// GitHub ships the full Actions surface, so all four methods are real.
//
// GitHub Actions endpoints used here:
//
//	GET  /repos/{o}/{r}/actions/runs                  — list runs (envelope: {total_count, workflow_runs})
//	GET  /repos/{o}/{r}/actions/runs/{run_id}         — single run
//	GET  /repos/{o}/{r}/actions/runs/{run_id}/jobs    — run's jobs (envelope: {total_count, jobs})
//	GET  /repos/{o}/{r}/actions/jobs/{job_id}/logs    — one job's plain-text log (302 → blob storage)
//	POST /repos/{o}/{r}/actions/runs/{run_id}/rerun   — re-trigger a run
//
// On ID semantics: GitHub has a single run identifier. The integer in
// the UI URL (`…/actions/runs/1234567890`) IS the API's `run_id` — there
// is no Forgejo-style split between an internal database ID and a
// user-facing run number. gaia therefore sets both types.WorkflowRun.ID
// and types.WorkflowRun.RunID to the same value, and no resolution
// round-trip is needed before a by-id fetch.
//
// On status: GitHub splits "did it finish" (status: queued / in_progress
// / completed) from "did it pass" (conclusion: success / failure /
// cancelled / skipped / …). Forgejo unifies both into one Status string.
// gaia reconciles to match Forgejo at the boundary: once a run/job is
// complete the conclusion becomes the Status; while it's still running
// the in-flight status is the Status. This keeps types.WorkflowRun.Status
// semantically identical across both providers.
package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// --- Wire types --------------------------------------------------------

// apiWorkflowRun mirrors the fields gaia trims from GitHub's
// workflow-run object. GitHub's run object is large; only the fields
// that map onto types.WorkflowRun are declared.
type apiWorkflowRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	DisplayTitle string    `json:"display_title"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Actor        apiUser   `json:"actor"`
}

func (a *apiWorkflowRun) toRun() types.WorkflowRun {
	return types.WorkflowRun{
		// GitHub's run_id is the user-facing run number, so ID == RunID.
		ID:           a.ID,
		RunID:        a.ID,
		WorkflowName: a.Name,
		Event:        a.Event,
		Status:       reconcileRunStatus(a.Status, a.Conclusion),
		Branch:       a.HeadBranch,
		HeadSHA:      a.HeadSHA,
		HeadMessage:  a.DisplayTitle,
		Actor:        types.User{Login: a.Actor.Login},
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
		HTMLURL:      a.HTMLURL,
	}
}

type apiWorkflowRunList struct {
	TotalCount   int              `json:"total_count"`
	WorkflowRuns []apiWorkflowRun `json:"workflow_runs"`
}

// apiJob mirrors the trimmed fields of GitHub's Actions-job object.
type apiJob struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

func (a *apiJob) toJob() types.WorkflowJob {
	return types.WorkflowJob{
		ID:          a.ID,
		Name:        a.Name,
		Status:      reconcileRunStatus(a.Status, a.Conclusion),
		StartedAt:   a.StartedAt,
		CompletedAt: a.CompletedAt,
	}
}

type apiJobList struct {
	TotalCount int      `json:"total_count"`
	Jobs       []apiJob `json:"jobs"`
}

// reconcileRunStatus folds GitHub's two-field status+conclusion model
// into the single Status string Forgejo (and therefore the trimmed
// type) uses: the conclusion wins once it's set (the run/job is
// complete), otherwise the in-flight status is reported.
func reconcileRunStatus(status, conclusion string) string {
	if conclusion != "" {
		return conclusion
	}
	return status
}

// --- Provider methods --------------------------------------------------

// ListWorkflowRuns returns recent workflow runs for the repo, newest
// first. opts.Status maps to GitHub's `status` query param (which
// accepts both status and conclusion values, e.g. "in_progress",
// "success", "failure"); opts.Branch maps to `branch`.
func (p *Provider) ListWorkflowRuns(ctx context.Context, owner, repo string, opts provider.ListWorkflowRunsOptions) ([]types.WorkflowRun, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Branch != "" {
		q.Set("branch", opts.Branch)
	}

	path := fmt.Sprintf("/repos/%s/%s/actions/runs?%s", owner, repo, q.Encode())
	var wrap apiWorkflowRunList
	if err := p.client.Get(ctx, path, &wrap); err != nil {
		return nil, nil, err
	}
	out := make([]types.WorkflowRun, 0, len(wrap.WorkflowRuns))
	for i := range wrap.WorkflowRuns {
		out = append(out, wrap.WorkflowRuns[i].toRun())
	}
	return out, makePage(len(wrap.WorkflowRuns), limit, opts.Cursor), nil
}

// GetWorkflowRun fetches one run by its run ID (the integer in the UI
// URL). When opts.WithJobs is set, the run's jobs are fetched and
// inlined via a second request.
func (p *Provider) GetWorkflowRun(ctx context.Context, owner, repo string, runID int64, opts provider.GetWorkflowRunOptions) (*types.WorkflowRun, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", owner, repo, runID)
	var raw apiWorkflowRun
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	run := raw.toRun()

	if opts.WithJobs {
		jobs, err := p.listRunJobs(ctx, owner, repo, runID)
		if err != nil {
			return nil, err
		}
		run.Jobs = make([]types.WorkflowJob, 0, len(jobs))
		for i := range jobs {
			run.Jobs = append(run.Jobs, jobs[i].toJob())
		}
	}
	return &run, nil
}

// listRunJobs fetches the jobs for a run. Returns the raw API jobs so
// callers can read conclusion (for FailedOnly filtering) before
// trimming to types.WorkflowJob.
func (p *Provider) listRunJobs(ctx context.Context, owner, repo string, runID int64) ([]apiJob, error) {
	// per_page=100 covers all but pathologically large runs in one hop;
	// matching the Forgejo provider's single-page job fetch.
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?per_page=100", owner, repo, runID)
	var wrap apiJobList
	if err := p.client.Get(ctx, path, &wrap); err != nil {
		return nil, err
	}
	return wrap.Jobs, nil
}

// GetWorkflowRunLogs returns per-job logs for a run. GitHub's run-level
// logs endpoint serves a zip; gaia instead fetches each job's plain-text
// log individually (`/actions/jobs/{job_id}/logs`) so the result carries
// the job's ID and name with no zip parsing. opts.FailedOnly restricts
// the fetch to jobs whose conclusion is "failure" — the common
// CI-triage case.
func (p *Provider) GetWorkflowRunLogs(ctx context.Context, owner, repo string, runID int64, opts provider.GetWorkflowRunLogsOptions) ([]types.WorkflowRunLogs, error) {
	jobs, err := p.listRunJobs(ctx, owner, repo, runID)
	if err != nil {
		return nil, err
	}
	out := make([]types.WorkflowRunLogs, 0, len(jobs))
	for i := range jobs {
		if opts.FailedOnly && jobs[i].Conclusion != "failure" {
			continue
		}
		path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobs[i].ID)
		body, err := p.client.GetRaw(ctx, path, "")
		if err != nil {
			return nil, err
		}
		out = append(out, types.WorkflowRunLogs{
			JobID:   jobs[i].ID,
			JobName: jobs[i].Name,
			Lines:   splitLogLines(string(body)),
		})
	}
	return out, nil
}

// splitLogLines splits a raw job log into lines, dropping a trailing
// empty line so the slice doesn't carry a spurious blank entry.
func splitLogLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

// RerunWorkflowRun re-triggers a run via GitHub's rerun endpoint. The
// rerun is asynchronous; GitHub returns 201 and the new attempt appears
// under the same run ID.
func (p *Provider) RerunWorkflowRun(ctx context.Context, owner, repo string, runID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/rerun", owner, repo, runID)
	return p.client.Post(ctx, path, nil, nil)
}
