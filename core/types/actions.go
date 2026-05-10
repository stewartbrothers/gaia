package types

import "time"

// WorkflowRun is the trimmed view of one Actions workflow run.
//
// Two IDs are surfaced:
//
//   - ID is the user-facing run number (the integer in the UI URL,
//     e.g. `…/actions/runs/356` → ID=356). Forgejo calls this
//     `index_in_repo` over the wire. Use this when cross-referencing
//     with what a human sees in the Actions UI.
//   - RunID is the internal database ID Forgejo uses for follow-up
//     API calls (e.g. fetching a single run). Agents that need to
//     hit the API for more detail should pass RunID, not ID.
//
// Forgejo (unlike GitHub) does not expose a separate "conclusion"
// field — a run's terminal outcome lives in Status (success, failure,
// cancelled, skipped, …) once the run completes. There is no
// Conclusion field on this struct for that reason; reading
// `run.Status` after `run.Status != "running" && run.Status !=
// "waiting"` is the agent-friendly way to branch on outcome.
//
// HeadMessage carries user-supplied commit message content and is
// tagged `gaia:"trust=external"` so the envelope marshaler wraps it
// in the injection-protection envelope the same way Issue.Body is
// treated (#146).
type WorkflowRun struct {
	// ID is the user-facing run number (Forgejo `index_in_repo`).
	// Matches the integer in the UI URL.
	ID int64 `json:"id"`
	// RunID is the internal Forgejo run identifier — required when
	// calling the run-by-id API endpoint for further detail.
	RunID        int64     `json:"run_id"`
	WorkflowName string    `json:"workflow_name"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	Branch       string    `json:"branch"`
	HeadSHA      string    `json:"head_sha"`
	HeadMessage  string    `json:"head_message,omitempty" gaia:"trust=external"`
	Actor        User      `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// HTMLURL points at the run's UI page. Useful when the API
	// can't fetch logs (Forgejo v15.x doesn't expose a logs
	// endpoint) — agents redirect humans here.
	HTMLURL string        `json:"html_url,omitempty"`
	Jobs    []WorkflowJob `json:"jobs,omitempty"`
}

// WorkflowJob is one job (task) within a workflow run.
//
// Forgejo's ActionTask API does not expose per-step status, so Steps
// is intentionally omitted; the field belongs to a future GitHub
// provider implementation that has richer per-step data.
type WorkflowJob struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// WorkflowRunLogs holds the log lines for one job in a workflow run.
// Each job's logs are grouped separately so agents can pull just the
// failed job without sifting through all output.
type WorkflowRunLogs struct {
	JobID   int64    `json:"job_id"`
	JobName string   `json:"job_name"`
	Lines   []string `json:"lines"`
}
