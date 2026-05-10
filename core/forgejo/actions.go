// Package forgejo: Actions workflow run inspection (#183).
//
// API shape verified against Forgejo v15.0.1 (gitea-1.22.0 compat) —
// the running instance behind https://git.stewartbrothers.com.au —
// using the published swagger spec, the v15.0.1 source tree at
// code.forgejo.org, and direct curl probes. The Forgejo Actions API
// surface is materially smaller than GitHub's, and the on-the-wire
// shape uses different field names. The notes below are the
// authoritative reference for this package — please don't pattern-
// match from GitHub.
//
// Forgejo Actions endpoints used here:
//
//	GET /repos/{o}/{r}/actions/runs                         — list runs
//	GET /repos/{o}/{r}/actions/runs/{run_id}                — single run by INTERNAL id
//	GET /repos/{o}/{r}/actions/runs?run_number={n}&limit=1  — resolve user-facing run number → run
//	GET /repos/{o}/{r}/actions/tasks                        — list ALL repo tasks (does NOT filter by run_id)
//
// Endpoints that DO NOT exist on Forgejo v15.0.1 (and therefore
// cannot be implemented yet, see #266 for logs, #267 for rerun):
//
//	/repos/{o}/{r}/actions/runs/{id}/jobs       — added in newer Forgejo
//	/repos/{o}/{r}/actions/runs/{id}/logs       — does not exist, ever
//	/repos/{o}/{r}/actions/tasks/{id}/logs      — fabrication; never existed
//	/repos/{o}/{r}/actions/runs/{id}/rerun      — does not exist via API
//
// Web UI routes for logs/rerun (e.g.
// `/{owner}/{repo}/actions/runs/{run}/jobs/{job}/attempt/{n}/logs`)
// require session-cookie authentication; PAT-based auth (the only
// kind gaia issues against the API) gets redirected to /user/login.
// Logs and rerun are therefore unavailable until upstream Forgejo
// adds an API endpoint, and the corresponding gaia methods return a
// clear "unsupported on this server version" error rather than
// fabricating a 404'ing path.
//
// On ID semantics: Forgejo workflow runs have two IDs.
//
//   - The internal database ID (the `id` field on the API response).
//     This is what the API requires when fetching a single run.
//   - The user-facing run number (the `index_in_repo` field on the
//     wire — the number that appears in the UI URL,
//     `…/actions/runs/362`). This is what humans see and reference.
//
// gaia surfaces both: types.WorkflowRun.ID is the user-facing number
// (matching the UI), and types.WorkflowRun.RunID is the internal one
// (needed for follow-up API calls). The CLI `gaia actions view <id>`
// and `gaia actions logs <id>` accept the user-facing number; the
// provider resolves to the internal ID transparently.
package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// --- Wire types --------------------------------------------------------

// apiActionRun mirrors Forgejo v15.0.1's ActionRun JSON shape exactly.
// See modules/structs/action.go in the Forgejo source. Note the
// non-obvious mappings: `index_in_repo` is the user-facing run
// number, `prettyref` is the branch, `commit_sha` is the head SHA,
// and there is no `conclusion` field — Status carries the terminal
// state.
type apiActionRun struct {
	// ID is the internal database ID. Required for downstream API
	// calls; not what the user sees in the UI URL.
	ID int64 `json:"id"`
	// Index is the user-facing run number — the integer in the UI
	// URL (`/actions/runs/362`).
	Index int64 `json:"index_in_repo"`
	// Title is the run's display title — usually the head commit
	// or PR title.
	Title string `json:"title"`
	// WorkflowID is the workflow file name (e.g. "ci.yml").
	WorkflowID string `json:"workflow_id"`
	// PrettyRef is the branch name without the `refs/heads/` prefix.
	PrettyRef string `json:"prettyref"`
	// CommitSHA is the head commit SHA the run executed against.
	CommitSHA string `json:"commit_sha"`
	// Event is the trigger event (e.g. "push", "pull_request").
	Event string `json:"event"`
	// Status carries both progress and terminal outcome:
	// "waiting", "running", "success", "failure", "cancelled",
	// "skipped", "blocked", "unknown". There is no separate
	// conclusion field.
	Status string `json:"status"`
	// Started is the time the run began executing.
	Started string `json:"started"`
	// Stopped is the time the run finished (zero while running).
	Stopped string `json:"stopped"`
	// Created is the time the run was queued.
	Created string `json:"created"`
	// Updated is the last update timestamp.
	Updated string `json:"updated"`
	// HTMLURL points at the run's UI page.
	HTMLURL string `json:"html_url"`
	// TriggerUser is the actor that triggered the run.
	TriggerUser struct {
		Login string `json:"login"`
	} `json:"trigger_user"`
}

type apiActionRunList struct {
	TotalCount   int            `json:"total_count"`
	WorkflowRuns []apiActionRun `json:"workflow_runs"`
}

// parseRunTime parses a Forgejo RFC3339 timestamp; returns zero on
// failure or empty input. Forgejo uses an offset format like
// "2026-05-10T11:32:08+10:00".
func parseRunTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (a *apiActionRun) toRun() types.WorkflowRun {
	return types.WorkflowRun{
		ID:           a.Index,
		RunID:        a.ID,
		WorkflowName: a.WorkflowID,
		Event:        a.Event,
		Status:       a.Status,
		Branch:       a.PrettyRef,
		HeadSHA:      a.CommitSHA,
		HeadMessage:  a.Title,
		Actor:        types.User{Login: a.TriggerUser.Login},
		CreatedAt:    parseRunTime(a.Created),
		UpdatedAt:    parseRunTime(a.Updated),
		HTMLURL:      a.HTMLURL,
	}
}

// apiActionTask mirrors Forgejo v15.0.1's ActionTask JSON shape
// (modules/structs/repo_actions.go). Note this differs from
// apiActionRun: tasks use `head_branch`/`head_sha`/`run_number`/
// `display_title`/`name`/`created_at`/`updated_at`/`run_started_at`
// — completely different field names from the run shape. There is
// no per-step output.
type apiActionTask struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	HeadBranch   string `json:"head_branch"`
	HeadSHA      string `json:"head_sha"`
	RunNumber    int64  `json:"run_number"`
	Event        string `json:"event"`
	DisplayTitle string `json:"display_title"`
	Status       string `json:"status"`
	WorkflowID   string `json:"workflow_id"`
	URL          string `json:"url"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	RunStartedAt string `json:"run_started_at"`
}

type apiActionTaskList struct {
	TotalCount   int             `json:"total_count"`
	WorkflowRuns []apiActionTask `json:"workflow_runs"`
}

func (a *apiActionTask) toJob() types.WorkflowJob {
	job := types.WorkflowJob{
		ID:     a.ID,
		Name:   a.Name,
		Status: a.Status,
	}
	if t := parseRunTime(a.RunStartedAt); !t.IsZero() {
		job.StartedAt = &t
	}
	if t := parseRunTime(a.UpdatedAt); !t.IsZero() && a.Status != "running" && a.Status != "waiting" {
		// Forgejo's task list doesn't expose a stopped time; the
		// closest proxy is `updated_at` once the task is in a
		// terminal state.
		job.CompletedAt = &t
	}
	return job
}

// --- Provider methods --------------------------------------------------

// ListWorkflowRuns returns recent workflow runs for the repo, newest
// first. The `branch` filter is mapped to Forgejo's `ref` query
// parameter (Forgejo expects fully-qualified refs internally; `ref`
// honours short branch names too, per its Form parsing).
func (p *Provider) ListWorkflowRuns(ctx context.Context, owner, repo string, opts provider.ListWorkflowRunsOptions) ([]types.WorkflowRun, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Branch != "" {
		// Forgejo's ListActionRuns accepts a `ref` query param
		// (matched against the run's stored ref). A bare branch
		// name works because Forgejo compares against the
		// shortened `prettyref` value; users referencing
		// `refs/heads/main` work too.
		q.Set("ref", opts.Branch)
	}

	path := fmt.Sprintf("/repos/%s/%s/actions/runs?%s", owner, repo, q.Encode())
	var raw apiActionRunList
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.WorkflowRun, 0, len(raw.WorkflowRuns))
	for i := range raw.WorkflowRuns {
		out = append(out, raw.WorkflowRuns[i].toRun())
	}
	return out, makePage(len(raw.WorkflowRuns), limit, opts.Cursor), nil
}

// GetWorkflowRun fetches one run. The runID parameter is the
// USER-FACING run number (Forgejo's `index_in_repo`) — what an
// agent sees in `gaia actions list`'s id column or in the UI URL.
// The provider resolves it to the internal ID via the
// `?run_number=N&limit=1` filter and then issues the by-id call.
//
// When opts.WithJobs is true a second request lists the run's tasks
// (jobs) and inlines them. Note: Forgejo v15.0.1's tasks endpoint
// does NOT filter by run_id — it returns ALL tasks for the repo.
// gaia therefore filters in-process by matching task.RunNumber to
// the request's run number.
func (p *Provider) GetWorkflowRun(ctx context.Context, owner, repo string, runNumber int64, opts provider.GetWorkflowRunOptions) (*types.WorkflowRun, error) {
	internalID, err := p.resolveRunInternalID(ctx, owner, repo, runNumber)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", owner, repo, internalID)
	var raw apiActionRun
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	run := raw.toRun()

	if opts.WithJobs {
		jobs, err := p.listTasksForRun(ctx, owner, repo, runNumber)
		if err != nil {
			return nil, err
		}
		run.Jobs = jobs
	}
	return &run, nil
}

// resolveRunInternalID looks up the internal run ID by user-facing
// run number. If the caller already has the internal ID handy this
// extra round-trip is wasted; future revisions may accept either,
// but the contract today is "give me the user-facing number".
func (p *Provider) resolveRunInternalID(ctx context.Context, owner, repo string, runNumber int64) (int64, error) {
	q := url.Values{}
	q.Set("run_number", strconv.FormatInt(runNumber, 10))
	q.Set("limit", "1")
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?%s", owner, repo, q.Encode())
	var raw apiActionRunList
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return 0, err
	}
	if len(raw.WorkflowRuns) == 0 {
		return 0, exitcode.Errorf(exitcode.NotFound,
			"forgejo: no workflow run with run_number=%d in %s/%s", runNumber, owner, repo)
	}
	return raw.WorkflowRuns[0].ID, nil
}

// listTasksForRun fetches up to one page of tasks for a repo and
// filters them in-process to the run identified by runNumber. The
// API does not accept a run_id filter (the parameter is silently
// ignored by Forgejo v15.0.1), so we have to filter client-side.
//
// One page (default 50) is enough for almost all real-world runs;
// runs with more than 50 jobs are exceptionally rare and out of
// scope for this fix. If they show up in practice we'll add
// pagination here.
func (p *Provider) listTasksForRun(ctx context.Context, owner, repo string, runNumber int64) ([]types.WorkflowJob, error) {
	q := url.Values{}
	q.Set("limit", "50")
	path := fmt.Sprintf("/repos/%s/%s/actions/tasks?%s", owner, repo, q.Encode())
	var raw apiActionTaskList
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]types.WorkflowJob, 0)
	for i := range raw.WorkflowRuns {
		if raw.WorkflowRuns[i].RunNumber != runNumber {
			continue
		}
		out = append(out, raw.WorkflowRuns[i].toJob())
	}
	return out, nil
}

// GetWorkflowRunLogs is currently unsupported on Forgejo. Forgejo
// v15.0.1's API does not expose Actions log content via any
// endpoint reachable with a personal access token. The web UI route
// that serves logs requires session-cookie authentication; gaia's
// PAT-based auth gets redirected to /user/login and yields HTML.
//
// Tracked upstream: this method will be implemented when Forgejo
// adds an API path. See gap issue #266.
//
// Until then this method returns an exitcode.Generic error with the
// run's UI URL embedded so the user can fetch logs manually. The
// CLI's `gaia actions logs` surface knows how to format that.
func (p *Provider) GetWorkflowRunLogs(ctx context.Context, owner, repo string, runNumber int64, _ provider.GetWorkflowRunLogsOptions) ([]types.WorkflowRunLogs, error) {
	// Best-effort: try to surface the run's UI URL so the error
	// message is actionable.
	htmlURL := fmt.Sprintf("/%s/%s/actions/runs/%d", owner, repo, runNumber)
	if run, err := p.GetWorkflowRun(ctx, owner, repo, runNumber, provider.GetWorkflowRunOptions{}); err == nil && run != nil && run.HTMLURL != "" {
		htmlURL = run.HTMLURL
	}
	return nil, exitcode.Errorf(exitcode.Generic,
		"forgejo: action run logs are not exposed via the Forgejo v15 API. "+
			"View logs in the UI: %s. Tracked upstream as gap #266.",
		htmlURL)
}

// RerunWorkflowRun is currently unsupported on Forgejo. Forgejo
// v15.0.1's API does not expose a rerun endpoint; rerun is only
// available via the web UI (POST `/{owner}/{repo}/actions/runs/{n}/rerun`)
// which requires session-cookie auth. Tracked as gap #267.
func (p *Provider) RerunWorkflowRun(_ context.Context, owner, repo string, runNumber int64) error {
	return exitcode.Errorf(exitcode.Generic,
		"forgejo: rerunning workflow runs is not exposed via the Forgejo v15 API. "+
			"Re-trigger via the UI: /%s/%s/actions/runs/%d. Tracked upstream as gap #267.",
		owner, repo, runNumber)
}
