package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// mcpActionRun returns a Forgejo v15.0.1-shape ActionRun JSON object
// (verified against the running instance + the v15.0.1 source tree at
// code.forgejo.org). Note the wire shape: `index_in_repo` is the
// user-facing run number, `id` is the internal db ID, `prettyref` is
// the branch, `commit_sha` is the head SHA, `title` is the head
// message, `trigger_user.login` is the actor, `created`/`updated`
// (no `_at` suffix) are timestamps, and there is NO `conclusion`
// field — Status carries terminal outcome.
func mcpActionRun(id, indexInRepo int64, status, branch string) map[string]any {
	return map[string]any{
		"id":            id,
		"index_in_repo": indexInRepo,
		"title":         "fix: test",
		"workflow_id":   "ci.yml",
		"event":         "push",
		"status":        status,
		"prettyref":     branch,
		"commit_sha":    "abc1234",
		"trigger_user":  map[string]any{"login": "alice"},
		"created":       "2026-04-01T10:00:00Z",
		"updated":       "2026-04-01T10:05:00Z",
		"html_url":      "https://example.test/o/r/actions/runs/" + map[bool]string{true: "362", false: "0"}[indexInRepo == 362],
	}
}

func TestActionsListRunsTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"workflow_runs": []map[string]any{
				mcpActionRun(1706, 362, "success", "main"),
				mcpActionRun(1705, 361, "failure", "feature/x"),
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
		case "/repos/o/r/actions/runs":
			// Resolve step: caller passed the user-facing run
			// number (362); the provider issues
			// `?run_number=362&limit=1`.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count":   1,
				"workflow_runs": []map[string]any{mcpActionRun(1706, 362, "success", "main")},
			})
		case "/repos/o/r/actions/runs/1706":
			// Fetch by internal ID (1706).
			_ = json.NewEncoder(w).Encode(mcpActionRun(1706, 362, "success", "main"))
		case "/repos/o/r/actions/tasks":
			// WithJobs: tasks endpoint returns ALL repo
			// tasks (Forgejo v15 doesn't filter by run_id).
			// The provider filters in-process by RunNumber.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"workflow_runs": []map[string]any{
					{
						"id":          100,
						"name":        "build",
						"run_number":  362,
						"status":      "success",
						"workflow_id": "ci.yml",
						"created_at":  "2026-04-01T10:00:00Z",
						"updated_at":  "2026-04-01T10:05:00Z",
					},
				},
			})
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleActionsViewRun, map[string]any{
		"repo": "o/r", "run_id": float64(362),
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

// TestActionsGetLogsToolUnsupported confirms the MCP tool surfaces the
// unsupported-on-this-server error rather than fabricating an API
// path. Regression for #262.
func TestActionsGetLogsToolUnsupported(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		// Resolution + by-id fetch are allowed (the provider
		// embeds the run's html_url in the error). Anything
		// else is a fabrication.
		switch r.URL.Path {
		case "/repos/o/r/actions/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count":   1,
				"workflow_runs": []map[string]any{mcpActionRun(1706, 362, "failure", "main")},
			})
		case "/repos/o/r/actions/runs/1706":
			_ = json.NewEncoder(w).Encode(mcpActionRun(1706, 362, "failure", "main"))
		default:
			t.Errorf("unexpected path: %q (logs API doesn't exist on Forgejo v15)", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	pinBuilder(t, p)

	res, _ := callTool(context.Background(), handleActionsGetLogs, map[string]any{
		"repo": "o/r", "run_id": float64(362), "failed_only": true,
	})
	if !res.IsError {
		t.Fatal("logs must surface as an MCP error result on Forgejo v15")
	}
	txt := resultText(t, res)
	if !strings.Contains(txt, "not exposed") {
		t.Errorf("error must explain the limitation; got %q", txt)
	}
}

func TestActionsRerunToolRequiresRunID(t *testing.T) {
	res, _ := callTool(context.Background(), handleActionsRerun, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing run_id must error")
	}
}

// TestActionsRerunToolUnsupported: rerun is not exposed via the
// Forgejo v15 API. Provider must return an unsupported error without
// hitting any endpoint.
func TestActionsRerunToolUnsupported(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("rerun tool must not hit the API on Forgejo v15; got %s %s", r.Method, r.URL.Path)
	})
	pinBuilder(t, p)

	res, _ := callTool(context.Background(), handleActionsRerun, map[string]any{
		"repo": "o/r", "run_id": float64(362),
	})
	if !res.IsError {
		t.Fatal("rerun must surface as an MCP error result on Forgejo v15")
	}
	txt := resultText(t, res)
	if !strings.Contains(txt, "not exposed") {
		t.Errorf("error must explain the limitation; got %q", txt)
	}
}
