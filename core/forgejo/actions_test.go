package forgejo_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// runJSON returns a minimal Forgejo workflow-run JSON object suitable
// for use in both list and single-get responses.
func runJSON(id int64, name, event, status, conclusion, branch, sha string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        name,
		"event":       event,
		"status":      status,
		"conclusion":  conclusion,
		"head_branch": branch,
		"head_sha":    sha,
		"head_commit": map[string]any{
			"message": "fix: something important",
		},
		"trigger_actor": map[string]any{
			"login": "alice",
		},
		"created_at": "2026-04-01T10:00:00Z",
		"updated_at": "2026-04-01T10:05:00Z",
	}
}

func taskJSON(id int64, name, status, conclusion string) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"status":     status,
		"conclusion": conclusion,
		"steps": []map[string]any{
			{
				"name":       "Set up job",
				"status":     "completed",
				"conclusion": conclusion,
				"number":     1,
			},
		},
	}
}

// buildZip creates an in-memory ZIP archive with one entry per (name,
// content) pair. Used to simulate Forgejo's task-log endpoint.
func buildZip(entries map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			panic(fmt.Sprintf("buildZip Create %s: %v", name, err))
		}
		if _, err := f.Write([]byte(content)); err != nil {
			panic(fmt.Sprintf("buildZip Write %s: %v", name, err))
		}
	}
	if err := w.Close(); err != nil {
		panic(fmt.Sprintf("buildZip Close: %v", err))
	}
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// TestListWorkflowRuns
// ---------------------------------------------------------------------------

func TestListWorkflowRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/runs" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"workflow_runs": []map[string]any{
				runJSON(42, "CI", "push", "completed", "success", "main", "abc1234"),
				runJSON(41, "CI", "push", "completed", "failure", "feature/x", "dead000"),
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	runs, page, err := p.ListWorkflowRuns(context.Background(), "o", "r", provider.ListWorkflowRunsOptions{})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("count: %d", len(runs))
	}
	if runs[0].ID != 42 {
		t.Errorf("runs[0].ID: got %d, want 42", runs[0].ID)
	}
	if runs[1].Conclusion != "failure" {
		t.Errorf("runs[1].Conclusion: got %q, want failure", runs[1].Conclusion)
	}
	if runs[0].Actor.Login != "alice" {
		t.Errorf("runs[0].Actor.Login: got %q, want alice", runs[0].Actor.Login)
	}
	if page == nil {
		t.Error("page must not be nil")
	}
}

func TestListWorkflowRunsPassesStatusFilter(t *testing.T) {
	gotQuery := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":   0,
			"workflow_runs": []map[string]any{},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListWorkflowRuns(context.Background(), "o", "r", provider.ListWorkflowRunsOptions{
		Status: "failure",
		Branch: "main",
	})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if !strings.Contains(gotQuery, "status=failure") {
		t.Errorf("query %q must contain status=failure", gotQuery)
	}
	if !strings.Contains(gotQuery, "branch=main") {
		t.Errorf("query %q must contain branch=main", gotQuery)
	}
}

// ---------------------------------------------------------------------------
// TestGetWorkflowRun
// ---------------------------------------------------------------------------

func TestGetWorkflowRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/runs/42" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(runJSON(42, "CI", "push", "completed", "success", "main", "abc1234"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	run, err := p.GetWorkflowRun(context.Background(), "o", "r", 42, provider.GetWorkflowRunOptions{})
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if run.ID != 42 {
		t.Errorf("ID: got %d, want 42", run.ID)
	}
	if run.WorkflowName != "CI" {
		t.Errorf("WorkflowName: got %q, want CI", run.WorkflowName)
	}
	if run.HeadSHA != "abc1234" {
		t.Errorf("HeadSHA: got %q, want abc1234", run.HeadSHA)
	}
	if run.HeadMessage != "fix: something important" {
		t.Errorf("HeadMessage: got %q", run.HeadMessage)
	}
	if len(run.Jobs) != 0 {
		t.Errorf("Jobs must be empty without WithJobs; got %d", len(run.Jobs))
	}
}

func TestGetWorkflowRunWithJobs(t *testing.T) {
	taskCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/actions/runs/42":
			_ = json.NewEncoder(w).Encode(runJSON(42, "CI", "push", "completed", "success", "main", "abc1234"))
		case "/repos/o/r/actions/tasks":
			taskCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"workflow_runs": []map[string]any{
					taskJSON(100, "build", "completed", "success"),
				},
			})
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	run, err := p.GetWorkflowRun(context.Background(), "o", "r", 42, provider.GetWorkflowRunOptions{WithJobs: true})
	if err != nil {
		t.Fatalf("GetWorkflowRun WithJobs: %v", err)
	}
	if !taskCalled {
		t.Error("WithJobs must call the tasks endpoint")
	}
	if len(run.Jobs) != 1 {
		t.Fatalf("Jobs: got %d, want 1", len(run.Jobs))
	}
	if run.Jobs[0].ID != 100 {
		t.Errorf("Jobs[0].ID: got %d, want 100", run.Jobs[0].ID)
	}
	if len(run.Jobs[0].Steps) != 1 {
		t.Errorf("Jobs[0].Steps count: got %d, want 1", len(run.Jobs[0].Steps))
	}
}

func TestGetWorkflowRunNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetWorkflowRun(context.Background(), "o", "r", 99, provider.GetWorkflowRunOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound", got)
	}
}

func TestGetWorkflowRunAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetWorkflowRun(context.Background(), "o", "r", 1, provider.GetWorkflowRunOptions{})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth", got)
	}
}

// ---------------------------------------------------------------------------
// TestGetWorkflowRunLogs
// ---------------------------------------------------------------------------

func TestGetWorkflowRunLogs(t *testing.T) {
	// Two tasks: both successful. Logs should be returned for both.
	tasksHandled := false
	logsHandled := [2]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/actions/tasks" && r.URL.Query().Get("run_id") == "42":
			tasksHandled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 2,
				"workflow_runs": []map[string]any{
					taskJSON(10, "build", "completed", "success"),
					taskJSON(11, "test", "completed", "failure"),
				},
			})
		case r.URL.Path == "/repos/o/r/actions/tasks/10/logs":
			logsHandled[0] = true
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(buildZip(map[string]string{
				"1_Set up job.txt": "step output line 1\nstep output line 2\n",
			}))
		case r.URL.Path == "/repos/o/r/actions/tasks/11/logs":
			logsHandled[1] = true
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(buildZip(map[string]string{
				"1_Run tests.txt": "FAIL: TestFoo\npanic: nil pointer\n",
			}))
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	logs, err := p.GetWorkflowRunLogs(context.Background(), "o", "r", 42, provider.GetWorkflowRunLogsOptions{})
	if err != nil {
		t.Fatalf("GetWorkflowRunLogs: %v", err)
	}
	if !tasksHandled {
		t.Error("must call tasks endpoint")
	}
	if !logsHandled[0] || !logsHandled[1] {
		t.Errorf("log calls: %v", logsHandled)
	}
	if len(logs) != 2 {
		t.Fatalf("log count: got %d, want 2", len(logs))
	}
}

func TestGetWorkflowRunLogsFailedOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/actions/tasks" && r.URL.Query().Get("run_id") == "42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 2,
				"workflow_runs": []map[string]any{
					taskJSON(10, "build", "completed", "success"),
					taskJSON(11, "test", "completed", "failure"),
				},
			})
		case r.URL.Path == "/repos/o/r/actions/tasks/11/logs":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(buildZip(map[string]string{
				"1_Run tests.txt": "FAIL: TestFoo\n",
			}))
		case r.URL.Path == "/repos/o/r/actions/tasks/10/logs":
			t.Error("FailedOnly must not fetch logs for successful jobs")
			w.WriteHeader(500)
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	logs, err := p.GetWorkflowRunLogs(context.Background(), "o", "r", 42, provider.GetWorkflowRunLogsOptions{FailedOnly: true})
	if err != nil {
		t.Fatalf("GetWorkflowRunLogs FailedOnly: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("log count: got %d, want 1 (failed only)", len(logs))
	}
	if logs[0].JobName != "test" {
		t.Errorf("JobName: got %q, want test", logs[0].JobName)
	}
	if len(logs[0].Lines) == 0 {
		t.Error("Lines must not be empty")
	}
}

// ---------------------------------------------------------------------------
// TestRerunWorkflowRun
// ---------------------------------------------------------------------------

func TestRerunWorkflowRun(t *testing.T) {
	postPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		postPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.RerunWorkflowRun(context.Background(), "o", "r", 42); err != nil {
		t.Fatalf("RerunWorkflowRun: %v", err)
	}
	if postPath != "/repos/o/r/actions/runs/42/rerun" {
		t.Errorf("POST path: got %q", postPath)
	}
}
