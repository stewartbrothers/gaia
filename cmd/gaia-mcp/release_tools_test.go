package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
)

func releaseJSON(id int64, tag, name string, draft, pre bool) map[string]any {
	return map[string]any{
		"id":               id,
		"tag_name":         tag,
		"name":             name,
		"body":             "notes",
		"draft":            draft,
		"prerelease":       pre,
		"author":           map[string]any{"login": "alice"},
		"target_commitish": "main",
		"created_at":       "2026-04-01T00:00:00Z",
		"published_at":     "2026-04-01T01:00:00Z",
	}
}

func TestReleaseListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			releaseJSON(1, "v1.0.0", "First", false, false),
			releaseJSON(2, "v0.9.0-rc1", "RC", false, true),
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleReleaseList, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 2 {
		t.Fatalf("count: %d", len(arr))
	}
}

func TestReleaseViewToolRequiresTag(t *testing.T) {
	res, _ := callTool(context.Background(), handleReleaseView, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing tag must error")
	}
}

func TestReleaseViewTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases/tags/v1.0.0" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(releaseJSON(1, "v1.0.0", "First", false, false))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleReleaseView, map[string]any{
		"repo": "o/r", "tag": "v1.0.0",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestReleaseCreateTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		_ = json.NewEncoder(w).Encode(releaseJSON(99, "v2.0.0", "Big", false, false))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleReleaseCreate, map[string]any{
		"repo": "o/r", "tag": "v2.0.0", "name": "Big",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestReleaseCreateRequiresTag(t *testing.T) {
	res, _ := callTool(context.Background(), handleReleaseCreate, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing tag must error")
	}
}

func TestReleaseEditTriBool(t *testing.T) {
	patchHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(releaseJSON(7, "v1.0.0", "Old", false, false))
		case http.MethodPatch:
			atomic.AddInt32(&patchHits, 1)
			_ = json.NewEncoder(w).Encode(releaseJSON(7, "v1.0.0", "Old", false, true))
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleReleaseEdit, map[string]any{
		"repo": "o/r", "tag": "v1.0.0", "prerelease": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&patchHits) != 1 {
		t.Errorf("expected 1 PATCH; got %d", patchHits)
	}
}

func TestReleaseDeletePreview(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(_ http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteHits, 1)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleReleaseDelete, map[string]any{
		"repo": "o/r", "tag": "v1.0.0",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 0 {
		t.Errorf("preview must not DELETE; got %d", deleteHits)
	}
}

func TestReleaseDeleteWithConfirm(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(releaseJSON(7, "v1.0.0", "x", false, false))
		case http.MethodDelete:
			atomic.AddInt32(&deleteHits, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleReleaseDelete, map[string]any{
		"repo": "o/r", "tag": "v1.0.0", "confirm": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 1 {
		t.Errorf("confirm=true must DELETE; got %d", deleteHits)
	}
}
