package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeCheckRuns(runs []map[string]string) map[string]any {
	arr := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		arr = append(arr, map[string]any{
			"name":       r["name"],
			"status":     r["status"],
			"conclusion": r["conclusion"],
		})
	}
	return map[string]any{"total_count": len(runs), "check_runs": arr}
}

// TestGHGetCommitStatusSuccess verifies the happy path on GitHub: all
// check_runs completed with success conclusion → State "success".
func TestGHGetCommitStatusSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/check-runs") {
			t.Errorf("unexpected path: %q", r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(fakeCheckRuns([]map[string]string{
			{"name": "CI", "status": "completed", "conclusion": "success"},
		}))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetCommitStatus(context.Background(), "o", "r", "v0.2.7")
	if err != nil {
		t.Fatalf("GetCommitStatus: %v", err)
	}
	if got.State != "success" {
		t.Errorf("state: got %q want success", got.State)
	}
	if got.Successful != 1 || got.Total != 1 {
		t.Errorf("counts: successful=%d total=%d", got.Successful, got.Total)
	}
}

// TestGHGetCommitStatusPending verifies in_progress → State "pending".
func TestGHGetCommitStatusPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/check-runs") {
			t.Errorf("unexpected path: %q", r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(fakeCheckRuns([]map[string]string{
			{"name": "CI", "status": "in_progress", "conclusion": ""},
		}))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetCommitStatus(context.Background(), "o", "r", "v0.2.7")
	if err != nil {
		t.Fatalf("GetCommitStatus: %v", err)
	}
	if got.State != "pending" {
		t.Errorf("state: got %q want pending", got.State)
	}
}

// TestGHGetCommitStatusTagNameInPath verifies tag name placement in path.
func TestGHGetCommitStatusTagNameInPath(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(fakeCheckRuns(nil))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetCommitStatus(context.Background(), "o", "r", "v0.2.7")
	if err != nil {
		t.Fatalf("GetCommitStatus: %v", err)
	}
	if !strings.Contains(capturedPath, "v0.2.7") {
		t.Errorf("path: got %q, want it to contain tag name v0.2.7", capturedPath)
	}
}
