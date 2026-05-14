package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func mcpMilestoneJSON(id int64, title, state string) map[string]any {
	return map[string]any{
		"id":            id,
		"title":         title,
		"description":   "sprint",
		"state":         state,
		"open_issues":   3,
		"closed_issues": 2,
		"created_at":    "2026-04-01T00:00:00Z",
		"updated_at":    "2026-04-02T00:00:00Z",
		"due_on":        "2026-05-01T00:00:00Z",
	}
}

func TestMilestoneListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			mcpMilestoneJSON(1, "v0.4.0", "open"),
			mcpMilestoneJSON(2, "v0.5.0", "open"),
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleMilestoneList, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 2 {
		t.Fatalf("count: %d", len(arr))
	}
}

func TestMilestoneListToolPassesState(t *testing.T) {
	var gotState string
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotState = r.URL.Query().Get("state")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	pinBuilder(t, p)

	_, _ = callTool(context.Background(), handleMilestoneList, map[string]any{
		"repo":  "o/r",
		"state": "closed",
	})
	if gotState != "closed" {
		t.Errorf("state query: got %q", gotState)
	}
}

func TestMilestoneViewToolRequiresID(t *testing.T) {
	res, _ := callTool(context.Background(), handleMilestoneView, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing id must error")
	}
}

func TestMilestoneViewTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/milestones/7" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(mcpMilestoneJSON(7, "v0.4.0", "open"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleMilestoneView, map[string]any{
		"repo": "o/r", "id": float64(7),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestMilestoneCreateRequiresTitle(t *testing.T) {
	res, _ := callTool(context.Background(), handleMilestoneCreate, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing title must error")
	}
}

func TestMilestoneCreateTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		_ = json.NewEncoder(w).Encode(mcpMilestoneJSON(99, "Sprint 23", "open"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleMilestoneCreate, map[string]any{
		"repo":  "o/r",
		"title": "Sprint 23",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestMilestoneCreateBadDueOn(t *testing.T) {
	res, _ := callTool(context.Background(), handleMilestoneCreate, map[string]any{
		"repo":   "o/r",
		"title":  "x",
		"due_on": "not-a-date",
	})
	if !res.IsError {
		t.Error("bad due_on must error")
	}
}

func TestMilestoneEditTool(t *testing.T) {
	patchHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			atomic.AddInt32(&patchHits, 1)
		}
		_ = json.NewEncoder(w).Encode(mcpMilestoneJSON(7, "updated", "closed"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleMilestoneEdit, map[string]any{
		"repo": "o/r", "id": float64(7), "state": "closed",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&patchHits) != 1 {
		t.Errorf("expected 1 PATCH; got %d", patchHits)
	}
}

func TestMilestoneDeletePreview(t *testing.T) {
	deletes := int32(0)
	p, _ := fakeForgeProvider(t, func(_ http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deletes, 1)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleMilestoneDelete, map[string]any{
		"repo": "o/r", "id": float64(7),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deletes) != 0 {
		t.Errorf("preview must not DELETE; got %d", deletes)
	}
	if !strings.Contains(resultText(t, res), "would_delete") {
		t.Errorf("preview must mention would_delete; got %s", resultText(t, res))
	}
}

func TestMilestoneDeleteConfirmed(t *testing.T) {
	deletes := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deletes, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleMilestoneDelete, map[string]any{
		"repo": "o/r", "id": float64(7), "confirm": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deletes) != 1 {
		t.Errorf("confirm=true must DELETE; got %d", deletes)
	}
}

func TestMilestoneIssuesTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("milestones") != "7" {
			t.Errorf("milestones query: %q", r.URL.Query().Get("milestones"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 100, "title": "x", "state": "open",
				"user":       map[string]any{"login": "a"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleMilestoneIssues, map[string]any{
		"repo": "o/r", "id": float64(7),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Fatalf("count: %d", len(arr))
	}
}
