package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// mcpFakeIssueJSON is the wire shape gaia decodes Forgejo issues
// from. The dep tools return []Issue or *Issue, so we mock the same.
func mcpFakeIssueJSON(n int, title, state string) map[string]any {
	return map[string]any{
		"number": n, "title": title, "state": state,
		"user":       map[string]any{"login": "alice"},
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-02T00:00:00Z",
	}
}

func TestIssueDepListToolBlockers(t *testing.T) {
	var path string
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode([]map[string]any{
			mcpFakeIssueJSON(7, "blocker", "open"),
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueDepList, map[string]any{
		"repo":   "o/r",
		"number": float64(42),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if path != "/repos/o/r/issues/42/dependencies" {
		t.Errorf("default direction should hit /dependencies; got %q", path)
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Errorf("expected 1 issue; got %d", len(arr))
	}
}

func TestIssueDepListToolBlocking(t *testing.T) {
	var path string
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	pinBuilder(t, p)

	_, _ = callTool(context.Background(), handleIssueDepList, map[string]any{
		"repo":      "o/r",
		"number":    float64(42),
		"direction": "blocks",
	})
	if path != "/repos/o/r/issues/42/blocks" {
		t.Errorf("direction=blocks should hit /blocks; got %q", path)
	}
}

func TestIssueDepListToolBadDirection(t *testing.T) {
	res, _ := callTool(context.Background(), handleIssueDepList, map[string]any{
		"repo":      "o/r",
		"number":    float64(42),
		"direction": "sideways",
	})
	if !res.IsError {
		t.Error("invalid direction must error")
	}
}

func TestIssueDepListToolRequiresNumber(t *testing.T) {
	res, _ := callTool(context.Background(), handleIssueDepList, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing number must error")
	}
}

// TestIssueDepAddToolBlockerFraming pins that blocker=7, number=42
// produces "7 blocks 42" — POST to /repos/o/r/issues/42/dependencies
// with body {"index": 7}.
func TestIssueDepAddToolBlockerFraming(t *testing.T) {
	var (
		gotPath string
		gotBody map[string]any
	)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(mcpFakeIssueJSON(7, "added", "open"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueDepAdd, map[string]any{
		"repo":    "o/r",
		"number":  float64(42),
		"blocker": float64(7),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if gotPath != "/repos/o/r/issues/42/dependencies" {
		t.Errorf("path: got %q", gotPath)
	}
	if int(gotBody["index"].(float64)) != 7 {
		t.Errorf("body.index: got %v, want 7", gotBody["index"])
	}
}

// TestIssueDepAddToolBlocksFraming pins the inverse: blocks=7
// number=42 means "42 blocks 7" → edge stored on 7's /dependencies.
func TestIssueDepAddToolBlocksFraming(t *testing.T) {
	var (
		gotPath string
		gotBody map[string]any
	)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(mcpFakeIssueJSON(42, "added", "open"))
	})
	pinBuilder(t, p)

	_, _ = callTool(context.Background(), handleIssueDepAdd, map[string]any{
		"repo":   "o/r",
		"number": float64(42),
		"blocks": float64(7),
	})
	if gotPath != "/repos/o/r/issues/7/dependencies" {
		t.Errorf("path: got %q, want /repos/o/r/issues/7/dependencies", gotPath)
	}
	if int(gotBody["index"].(float64)) != 42 {
		t.Errorf("body.index: got %v, want 42", gotBody["index"])
	}
}

func TestIssueDepAddToolMutuallyExclusive(t *testing.T) {
	res, _ := callTool(context.Background(), handleIssueDepAdd, map[string]any{
		"repo":    "o/r",
		"number":  float64(42),
		"blocker": float64(7),
		"blocks":  float64(8),
	})
	if !res.IsError {
		t.Error("blocker + blocks together must error")
	}
}

func TestIssueDepAddToolRequiresOneFlag(t *testing.T) {
	res, _ := callTool(context.Background(), handleIssueDepAdd, map[string]any{
		"repo":   "o/r",
		"number": float64(42),
	})
	if !res.IsError {
		t.Error("neither blocker nor blocks must error")
	}
}

func TestIssueDepRemoveTool(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueDepRemove, map[string]any{
		"repo":    "o/r",
		"number":  float64(42),
		"blocker": float64(7),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method: got %s, want DELETE", gotMethod)
	}
	if gotPath != "/repos/o/r/issues/42/dependencies" {
		t.Errorf("path: got %q", gotPath)
	}
	if int(gotBody["index"].(float64)) != 7 {
		t.Errorf("body.index: got %v, want 7", gotBody["index"])
	}
	if !strings.Contains(resultText(t, res), `"removed_edge_from":42`) {
		t.Errorf("expected confirmation envelope in result; got %q", resultText(t, res))
	}
}
