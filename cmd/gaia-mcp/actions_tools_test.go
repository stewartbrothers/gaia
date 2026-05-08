package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
)

// mcpRunJSON returns a minimal Forgejo workflow-run JSON object for MCP tests.
func mcpRunJSON(id int64, name, status, conclusion, branch string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        name,
		"event":       "push",
		"status":      status,
		"conclusion":  conclusion,
		"head_branch": branch,
		"head_sha":    "abc1234",
		"head_commit": map[string]any{"message": "fix: test"},
		"trigger_actor": map[string]any{
			"login": "alice",
		},
		"created_at": "2026-04-01T10:00:00Z",
		"updated_at": "2026-04-01T10:05:00Z",
	}
}

// buildTestZip creates an in-memory ZIP for MCP log tests.
func buildTestZip(entries map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			panic(fmt.Sprintf("buildTestZip Create %s: %v", name, err))
		}
		_, _ = f.Write([]byte(content))
	}
	_ = w.Close()
	return buf.Bytes()
}

func TestActionsListRunsTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"workflow_runs": []map[string]any{
				mcpRunJSON(42, "CI", "completed", "success", "main"),
				mcpRunJSON(41, "CI", "completed", "failure", "feature/x"),
			},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleActionsListRuns, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 2 {
		t.Errorf("count: %d, want 2", len(arr))
	}
}

func TestActionsViewRunToolRequiresRunID(t *testing.T) {
	res, _ := callTool(context.Background(), handleActionsViewRun, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing run_id must error")
	}
}

func TestActionsViewRunTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/actions/runs/42":
			_ = json.NewEncoder(w).Encode(mcpRunJSON(42, "CI", "completed", "success", "main"))
		case "/repos/o/r/actions/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"workflow_runs": []map[string]any{
					{"id": 100, "name": "build", "status": "completed", "conclusion": "success", "steps": []map[string]any{}},
				},
			})
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleActionsViewRun, map[string]any{
		"repo": "o/r", "run_id": float64(42),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestActionsGetLogsToolRequiresRunID(t *testing.T) {
	res, _ := callTool(context.Background(), handleActionsGetLogs, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing run_id must error")
	}
}

func TestActionsGetLogsTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/actions/tasks" && r.URL.Query().Get("run_id") == "42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"workflow_runs": []map[string]any{
					{"id": 10, "name": "test", "status": "completed", "conclusion": "failure", "steps": []map[string]any{}},
				},
			})
		case r.URL.Path == "/repos/o/r/actions/tasks/10/logs":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(buildTestZip(map[string]string{
				"1_Run tests.txt": "FAIL: TestFoo\n",
			}))
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleActionsGetLogs, map[string]any{
		"repo": "o/r", "run_id": float64(42), "failed_only": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	// Result should be plain text (not envelope JSON)
	txt := resultText(t, res)
	if txt == "" {
		t.Error("logs must not be empty")
	}
}

func TestActionsRerunToolRequiresRunID(t *testing.T) {
	res, _ := callTool(context.Background(), handleActionsRerun, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing run_id must error")
	}
}

func TestActionsRerunTool(t *testing.T) {
	postHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&postHits, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleActionsRerun, map[string]any{
		"repo": "o/r", "run_id": float64(42),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&postHits) != 1 {
		t.Errorf("expected 1 POST; got %d", postHits)
	}
}
