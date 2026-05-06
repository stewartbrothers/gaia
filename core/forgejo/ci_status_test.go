package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeCommitStatus(state string, statuses []map[string]string) map[string]any {
	arr := make([]map[string]any, 0, len(statuses))
	for _, s := range statuses {
		arr = append(arr, map[string]any{
			"state":   s["state"],
			"context": s["name"],
		})
	}
	return map[string]any{"state": state, "statuses": arr}
}

// TestGetCommitStatusSuccess verifies the happy path: all checks success.
func TestGetCommitStatusSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			t.Errorf("unexpected path: %q", r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(fakeCommitStatus("success",
			[]map[string]string{
				{"name": "CI / check", "state": "success"},
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

// TestGetCommitStatusPending verifies pending checks are mapped correctly.
func TestGetCommitStatusPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			t.Errorf("unexpected path: %q", r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(fakeCommitStatus("pending",
			[]map[string]string{
				{"name": "CI / check", "state": "pending"},
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
	if got.Pending != 1 {
		t.Errorf("pending count: got %d want 1", got.Pending)
	}
}

// TestGetCommitStatusEmpty verifies the no-checks case returns an empty
// state, not an error. Callers treat "" as "not yet started / pending".
func TestGetCommitStatusEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			t.Errorf("unexpected path: %q", r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "", "statuses": []any{}})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetCommitStatus(context.Background(), "o", "r", "v0.2.7")
	if err != nil {
		t.Fatalf("GetCommitStatus: %v", err)
	}
	if got.State != "" {
		t.Errorf("state: got %q want empty", got.State)
	}
	if got.Total != 0 {
		t.Errorf("total: got %d want 0", got.Total)
	}
}

// TestGetCommitStatusTagNameInPath verifies the tag name is correctly
// placed in the URL path (covers url.PathEscape usage).
func TestGetCommitStatusTagNameInPath(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(fakeCommitStatus("success", nil))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetCommitStatus(context.Background(), "o", "r", "v0.2.7")
	if err != nil {
		t.Fatalf("GetCommitStatus: %v", err)
	}
	if !strings.HasSuffix(capturedPath, "/v0.2.7/status") {
		t.Errorf("path: got %q want suffix /v0.2.7/status", capturedPath)
	}
}
