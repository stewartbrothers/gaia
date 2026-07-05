package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestIssueListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "first", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z"},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueList, map[string]any{
		"repo": "o/r",
	})
	if err != nil {
		t.Fatalf("handleIssueList: %v", err)
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Fatalf("count: %d", len(arr))
	}
}

func TestIssueListMissingRepoIsError(t *testing.T) {
	res, err := callTool(context.Background(), handleIssueList, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("expected IsError; got %+v", res)
	}
}

func TestIssueViewTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/42" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 42, "title": "t", "state": "open",
			"user":       map[string]any{"login": "a"},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-02T00:00:00Z",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueView, map[string]any{
		"repo":   "o/r",
		"number": float64(42),
	})
	if err != nil {
		t.Fatal(err)
	}
	d := envelopeData(t, res)
	if d["number"].(float64) != 42 {
		t.Errorf("number: %v", d["number"])
	}
}

func TestIssueCreateTool(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/labels"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 3, "name": "bug", "color": "red"},
			})
		default:
			captured, _ = io.ReadAll(r.Body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 99, "title": "hi", "state": "open",
				"user":       map[string]any{"login": "a"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-01T00:00:00Z",
			})
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueCreate, map[string]any{
		"repo": "o/r", "title": "hi", "body": "world",
		"labels": []any{"bug"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", resultText(t, res))
	}
	// Labels are sent as integer IDs after name resolution.
	if !strings.Contains(string(captured), `"title":"hi"`) ||
		!strings.Contains(string(captured), `"labels":[3]`) {
		t.Errorf("captured body: %s", captured)
	}
}

func TestIssueCreateMissingTitleIsError(t *testing.T) {
	res, _ := callTool(context.Background(), handleIssueCreate, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Errorf("expected IsError")
	}
}

func TestIssueEditTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: %q", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 86, "title": "t", "state": "closed",
			"user":       map[string]any{"login": "a"},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-01T00:00:00Z",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueEdit, map[string]any{
		"repo": "o/r", "number": float64(86), "state": "closed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if envelopeData(t, res)["state"] != "closed" {
		t.Errorf("expected closed; got %+v", envelopeData(t, res))
	}
}

func TestIssueCreateToolWithMilestone(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 100, "title": "hi", "state": "open",
			"user":       map[string]any{"login": "a"},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-01T00:00:00Z",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueCreate, map[string]any{
		"repo": "o/r", "title": "hi", "milestone": float64(9),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", resultText(t, res))
	}
	if !strings.Contains(string(captured), `"milestone":9`) {
		t.Errorf("captured body: %s", captured)
	}
}

func TestIssueEditToolSetsMilestone(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 87, "title": "t", "state": "open",
			"user":       map[string]any{"login": "a"},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-01T00:00:00Z",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueEdit, map[string]any{
		"repo": "o/r", "number": float64(87), "milestone": float64(4),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if !strings.Contains(string(captured), `"milestone":4`) {
		t.Errorf("captured body: %s", captured)
	}
}

// TestIssueEditToolClearsMilestone pins the tri-state contract on the
// MCP surface: an explicit milestone:0 must serialize (not be
// indistinguishable from an absent field) so Forgejo detaches the
// current milestone.
func TestIssueEditToolClearsMilestone(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 88, "title": "t", "state": "open",
			"user":       map[string]any{"login": "a"},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-01T00:00:00Z",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueEdit, map[string]any{
		"repo": "o/r", "number": float64(88), "milestone": float64(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if !strings.Contains(string(captured), `"milestone":0`) {
		t.Errorf("captured body: %s, want explicit milestone:0", captured)
	}
}

func TestIssueEditToolOmitsMilestoneWhenAbsent(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 89, "title": "t", "state": "closed",
			"user":       map[string]any{"login": "a"},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-01T00:00:00Z",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueEdit, map[string]any{
		"repo": "o/r", "number": float64(89), "state": "closed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if strings.Contains(string(captured), `"milestone"`) {
		t.Errorf("milestone should be omitted when absent; got %s", captured)
	}
}

func TestIssueCommentTool(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 9, "user": map[string]any{"login": "a"}, "body": "hello",
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-01T00:00:00Z",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleIssueComment, map[string]any{
		"repo": "o/r", "number": float64(42), "body": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if !strings.Contains(string(captured), `"body":"hello"`) {
		t.Errorf("body: %s", captured)
	}
}
