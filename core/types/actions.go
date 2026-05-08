package types

import "time"

// WorkflowRun is the trimmed view of one Actions workflow run.
//
// HeadMessage carries user-supplied commit message content and is
// tagged `gaia:"trust=external"` so the envelope marshaler wraps it
// in the injection-protection envelope the same way Issue.Body is
// treated (#146).
type WorkflowRun struct {
	ID           int64         `json:"id"`
	WorkflowName string        `json:"workflow_name"`
	Event        string        `json:"event"`
	Status       string        `json:"status"`
	Conclusion   string        `json:"conclusion,omitempty"`
	Branch       string        `json:"branch"`
	HeadSHA      string        `json:"head_sha"`
	HeadMessage  string        `json:"head_message,omitempty" gaia:"trust=external"`
	Actor        User          `json:"actor"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	Jobs         []WorkflowJob `json:"jobs,omitempty"`
}

// WorkflowJob is one job (task) within a workflow run.
type WorkflowJob struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Conclusion  string         `json:"conclusion,omitempty"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	Steps       []WorkflowStep `json:"steps,omitempty"`
}

// WorkflowStep is one step within a job.
type WorkflowStep struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	Number     int    `json:"number"`
}

// WorkflowRunLogs holds the log lines for one job in a workflow run.
// Each job's logs are grouped separately so agents can pull just the
// failed job without sifting through all output.
type WorkflowRunLogs struct {
	JobID   int64    `json:"job_id"`
	JobName string   `json:"job_name"`
	Lines   []string `json:"lines"`
}
