package forgejo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// fixturePath resolves a path under core/forgejo/testdata/actions/.
// All Actions fixtures are recorded responses from a real Forgejo
// v15.0.1 instance — never hand-stubbed shapes that "look right".
// See `core/forgejo/actions.go` for the field-name reference.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", "actions", name)
}

// readFixture returns the bytes of a recorded API response.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// ---------------------------------------------------------------------------
// TestListWorkflowRuns — confirms the user-facing run number (Forgejo's
// `index_in_repo`) is what surfaces as types.WorkflowRun.ID, and the
// internal database ID lands in RunID. This is the regression test for
// #261 — `gaia actions list` previously emitted the internal ID and that
// number didn't match the UI URL.
// ---------------------------------------------------------------------------

func TestListWorkflowRunsMapsRunNumberAsID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/runs" {
			t.Errorf("path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "runs-list.json"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	runs, page, err := p.ListWorkflowRuns(context.Background(), "o", "r", provider.ListWorkflowRunsOptions{})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("count: got %d, want 2", len(runs))
	}
	// First run in the recorded fixture: index_in_repo=362, id=1706.
	if runs[0].ID != 362 {
		t.Errorf("runs[0].ID (user-facing run number): got %d, want 362", runs[0].ID)
	}
	if runs[0].RunID != 1706 {
		t.Errorf("runs[0].RunID (internal db ID): got %d, want 1706", runs[0].RunID)
	}
	// All the other previously-zero fields the bug surfaced. These
	// being non-empty is the #263 regression test.
	if runs[0].WorkflowName != "mirror.yml" {
		t.Errorf("runs[0].WorkflowName: got %q, want mirror.yml", runs[0].WorkflowName)
	}
	if runs[0].Branch != "main" {
		t.Errorf("runs[0].Branch: got %q, want main", runs[0].Branch)
	}
	if runs[0].HeadSHA == "" {
		t.Error("runs[0].HeadSHA must be populated (regression for #263)")
	}
	if runs[0].Actor.Login == "" {
		t.Error("runs[0].Actor.Login must be populated (regression for #263)")
	}
	if runs[0].CreatedAt.IsZero() {
		t.Error("runs[0].CreatedAt must be populated (regression for #263)")
	}
	if runs[0].UpdatedAt.IsZero() {
		t.Error("runs[0].UpdatedAt must be populated (regression for #263)")
	}
	if runs[0].HTMLURL == "" {
		t.Error("runs[0].HTMLURL must be populated for the logs gap workaround")
	}
	if page == nil {
		t.Error("page must not be nil")
	}
}

func TestListWorkflowRunsPassesStatusAndRefFilters(t *testing.T) {
	gotQuery := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"total_count":0,"workflow_runs":[]}`))
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
	// Forgejo's filter is `ref`, NOT `branch` — verified against
	// the swagger spec for ListActionRuns.
	if !strings.Contains(gotQuery, "ref=main") {
		t.Errorf("query %q must contain ref=main (Forgejo's filter is ref, not branch)", gotQuery)
	}
}

// ---------------------------------------------------------------------------
// TestGetWorkflowRun — exercises the run-number → internal-ID resolution
// step plus the field mapping. The CLI passes the user-facing run number;
// the provider issues `?run_number=N&limit=1` first, then `runs/{id}`
// with the resolved internal ID. This pattern is required because the
// Forgejo API only accepts the internal id in the path.
// ---------------------------------------------------------------------------

func TestGetWorkflowRunResolvesRunNumber(t *testing.T) {
	resolveCalled := false
	getCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/actions/runs":
			// resolution step — caller passed run_number=362
			if got := r.URL.Query().Get("run_number"); got != "362" {
				t.Errorf("run_number: got %q, want 362", got)
			}
			resolveCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "runs-list.json"))
		case "/repos/o/r/actions/runs/1706":
			// fetch by internal ID
			getCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "run-single.json"))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	run, err := p.GetWorkflowRun(context.Background(), "o", "r", 362, provider.GetWorkflowRunOptions{})
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if !resolveCalled {
		t.Error("must hit resolve step (run_number → internal id)")
	}
	if !getCalled {
		t.Error("must hit by-id step")
	}
	if run.ID != 362 {
		t.Errorf("ID (user-facing): got %d, want 362", run.ID)
	}
	if run.RunID != 1706 {
		t.Errorf("RunID (internal): got %d, want 1706", run.RunID)
	}
	if run.WorkflowName != "mirror.yml" {
		t.Errorf("WorkflowName: got %q, want mirror.yml", run.WorkflowName)
	}
	if run.HeadSHA == "" || run.Branch == "" || run.Actor.Login == "" {
		t.Error("real fields must be populated (regression for #263)")
	}
}

func TestGetWorkflowRunUnknownRunNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Empty list -> unresolved run number
		_, _ = w.Write([]byte(`{"total_count":0,"workflow_runs":[]}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetWorkflowRun(context.Background(), "o", "r", 99999, provider.GetWorkflowRunOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("unknown run_number must return NotFound, got %d", got)
	}
}

func TestGetWorkflowRunWithJobsFiltersByRunNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/actions/runs":
			// Resolution OR jobs-prep: branch on whether
			// run_number param is present.
			if r.URL.Query().Get("run_number") == "362" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(readFixture(t, "runs-list.json"))
			} else {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"message":"unexpected runs query"}`))
			}
		case "/repos/o/r/actions/runs/1706":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "run-single.json"))
		case "/repos/o/r/actions/tasks":
			// Forgejo v15.0.1: tasks endpoint doesn't filter
			// by run_id even if you pass it. The fixture
			// contains a task whose run_number is 362; the
			// provider filters in-process.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "tasks-list.json"))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	run, err := p.GetWorkflowRun(context.Background(), "o", "r", 362, provider.GetWorkflowRunOptions{WithJobs: true})
	if err != nil {
		t.Fatalf("GetWorkflowRun WithJobs: %v", err)
	}
	if len(run.Jobs) == 0 {
		t.Fatal("WithJobs must inline matching tasks; got 0 (filter probably wrong)")
	}
	for _, j := range run.Jobs {
		if j.Name == "" {
			t.Errorf("job name must be populated; got empty for %+v", j)
		}
	}
}

func TestGetWorkflowRunNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Make resolve return a row, but the by-id call 404s —
		// simulating a race or a deleted run.
		if r.URL.Path == "/repos/o/r/actions/runs" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "runs-list.json"))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetWorkflowRun(context.Background(), "o", "r", 362, provider.GetWorkflowRunOptions{})
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
// TestGetWorkflowRunLogs — Forgejo v15.0.1 has no log endpoint, so the
// provider returns a clear unsupported error rather than fabricating an
// API path that always 404s. This is the regression test for #262.
// ---------------------------------------------------------------------------

func TestGetWorkflowRunLogsUnsupported(t *testing.T) {
	requested := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested++
		// Resolve step is allowed (we want the html_url for the
		// error message). Anything else means the provider
		// fabricated an endpoint.
		switch r.URL.Path {
		case "/repos/o/r/actions/runs":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "runs-list.json"))
		case "/repos/o/r/actions/runs/1706":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "run-single.json"))
		default:
			t.Errorf("provider must NOT hit %s — Forgejo v15 has no logs API", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetWorkflowRunLogs(context.Background(), "o", "r", 362, provider.GetWorkflowRunLogsOptions{})
	if err == nil {
		t.Fatal("GetWorkflowRunLogs must return an unsupported error")
	}
	msg := err.Error()
	// The message must mention the limitation AND the html_url so
	// agents have an actionable next step.
	if !strings.Contains(msg, "not exposed") {
		t.Errorf("error must explain logs are unsupported on this Forgejo version; got %q", msg)
	}
	if !strings.Contains(msg, "https://") {
		t.Errorf("error must include the run's html_url so callers can grab logs manually; got %q", msg)
	}
	// Per #324: the dedicated NotImplemented exit code surfaces the
	// "unsupported on this forge version" signal cleanly.
	if got := exitcode.Of(err); got != exitcode.NotImplemented {
		t.Errorf("exit code: got %d, want NotImplemented(12)", got)
	}
}

// ---------------------------------------------------------------------------
// TestRerunWorkflowRun — Forgejo v15 also doesn't expose rerun. Same
// pattern: clean unsupported error rather than fabricated API path.
// ---------------------------------------------------------------------------

func TestRerunWorkflowRunUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("RerunWorkflowRun must not hit any API endpoint; got request to %s", r.URL.Path)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.RerunWorkflowRun(context.Background(), "o", "r", 362)
	if err == nil {
		t.Fatal("RerunWorkflowRun must return an unsupported error on Forgejo v15")
	}
	if !strings.Contains(err.Error(), "not exposed") {
		t.Errorf("error must explain rerun is unsupported; got %q", err)
	}
	if got := exitcode.Of(err); got != exitcode.NotImplemented {
		t.Errorf("exit code: got %d, want NotImplemented(12)", got)
	}
}
