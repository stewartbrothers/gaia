package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func TestListWorkflowRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/runs" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "failure" {
			t.Errorf("status filter: %q", got)
		}
		if got := r.URL.Query().Get("branch"); got != "main" {
			t.Errorf("branch filter: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"workflow_runs": []map[string]any{
				{
					"id": 101, "name": "CI", "head_branch": "main",
					"head_sha": "abc123", "display_title": "fix the thing",
					"event": "push", "status": "completed", "conclusion": "failure",
					"html_url":   "https://github.com/o/r/actions/runs/101",
					"created_at": "2026-04-01T00:00:00Z",
					"updated_at": "2026-04-01T01:00:00Z",
					"actor":      map[string]any{"login": "alice"},
				},
				{
					"id": 100, "name": "CI", "head_branch": "main",
					"status": "in_progress", "conclusion": "",
				},
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListWorkflowRuns(context.Background(), "o", "r", provider.ListWorkflowRunsOptions{
		Status: "failure", Branch: "main",
	})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 runs, got %d", len(got))
	}
	// ID == RunID on GitHub (single identifier).
	if got[0].ID != 101 || got[0].RunID != 101 {
		t.Errorf("ID/RunID: %+v", got[0])
	}
	// Completed run: conclusion wins as Status.
	if got[0].Status != "failure" {
		t.Errorf("completed run Status = %q, want failure", got[0].Status)
	}
	if got[0].WorkflowName != "CI" || got[0].HeadMessage != "fix the thing" || got[0].Actor.Login != "alice" {
		t.Errorf("trim: %+v", got[0])
	}
	// In-flight run: status wins as Status.
	if got[1].Status != "in_progress" {
		t.Errorf("in-flight run Status = %q, want in_progress", got[1].Status)
	}
}

func TestGetWorkflowRunWithJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/actions/runs/101":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 101, "name": "CI", "status": "completed", "conclusion": "success",
			})
		case "/repos/o/r/actions/runs/101/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"jobs": []map[string]any{
					{"id": 9, "name": "build", "status": "completed", "conclusion": "success",
						"started_at": "2026-04-01T00:00:00Z", "completed_at": "2026-04-01T00:05:00Z"},
				},
			})
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	run, err := p.GetWorkflowRun(context.Background(), "o", "r", 101, provider.GetWorkflowRunOptions{WithJobs: true})
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if run.Status != "success" {
		t.Errorf("run Status = %q, want success", run.Status)
	}
	if len(run.Jobs) != 1 || run.Jobs[0].Name != "build" || run.Jobs[0].Status != "success" {
		t.Fatalf("jobs: %+v", run.Jobs)
	}
	if run.Jobs[0].StartedAt == nil || run.Jobs[0].CompletedAt == nil {
		t.Errorf("job timestamps not populated: %+v", run.Jobs[0])
	}
}

func TestGetWorkflowRunNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.GetWorkflowRun(context.Background(), "o", "r", 1, provider.GetWorkflowRunOptions{}); exitcode.Of(err) != exitcode.NotFound {
		t.Errorf("want NotFound, got %v (code %d)", err, exitcode.Of(err))
	}
}

func TestGetWorkflowRunAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.GetWorkflowRun(context.Background(), "o", "r", 1, provider.GetWorkflowRunOptions{}); exitcode.Of(err) != exitcode.Auth {
		t.Errorf("want Auth, got %v (code %d)", err, exitcode.Of(err))
	}
}

func TestGetWorkflowRunLogsFailedOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/actions/runs/101/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 2,
				"jobs": []map[string]any{
					{"id": 9, "name": "build", "status": "completed", "conclusion": "success"},
					{"id": 10, "name": "test", "status": "completed", "conclusion": "failure"},
				},
			})
		case "/repos/o/r/actions/jobs/10/logs":
			_, _ = w.Write([]byte("step 1\nassertion failed: want 2 got 3\n"))
		default:
			t.Errorf("unexpected path: %q (FailedOnly should skip job 9)", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	logs, err := p.GetWorkflowRunLogs(context.Background(), "o", "r", 101, provider.GetWorkflowRunLogsOptions{FailedOnly: true})
	if err != nil {
		t.Fatalf("GetWorkflowRunLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("FailedOnly should yield 1 job, got %d", len(logs))
	}
	if logs[0].JobID != 10 || logs[0].JobName != "test" {
		t.Errorf("job identity: %+v", logs[0])
	}
	if len(logs[0].Lines) != 2 || logs[0].Lines[1] != "assertion failed: want 2 got 3" {
		t.Errorf("lines: %+v", logs[0].Lines)
	}
}

func TestRerunWorkflowRun(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/actions/runs/101/rerun" {
			t.Errorf("method/path: %s %q", r.Method, r.URL.Path)
		}
		hit = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.RerunWorkflowRun(context.Background(), "o", "r", 101); err != nil {
		t.Fatalf("RerunWorkflowRun: %v", err)
	}
	if !hit {
		t.Error("rerun endpoint not called")
	}
}
