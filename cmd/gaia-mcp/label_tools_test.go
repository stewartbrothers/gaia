package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLabelListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "bug", "color": "ff0000", "description": "broken"},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleLabelList, map[string]any{"repo": "o/r"})
	if err != nil {
		t.Fatal(err)
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Fatalf("count: %d", len(arr))
	}
}

// TestLabelListToolNameFilter pins #328 at the MCP level — name
// arg triggers the same client-side substring filter the CLI uses.
func TestLabelListToolNameFilter(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "bug"},
			{"id": 2, "name": "priority/high"},
			{"id": 3, "name": "priority/low"},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleLabelList, map[string]any{
		"repo": "o/r",
		"name": "priority",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 2 {
		t.Errorf("expected 2 priority labels; got %d", len(arr))
	}
}

func TestLabelCreateTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 5, "name": "release", "color": "00ff00", "description": "",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleLabelCreate, map[string]any{
		"repo": "o/r", "name": "release", "color": "00ff00",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestLabelCreateMissingFields(t *testing.T) {
	for _, args := range []map[string]any{
		{"repo": "o/r"},
		{"repo": "o/r", "name": "x"},
	} {
		res, _ := callTool(context.Background(), handleLabelCreate, args)
		if !res.IsError {
			t.Errorf("expected error for %+v", args)
		}
	}
}

func TestLabelEditLooksUpByName(t *testing.T) {
	patchHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 7, "name": "bug", "color": "ff0000", "description": ""},
			})
		case r.Method == http.MethodPatch:
			atomic.AddInt32(&patchHits, 1)
			if !strings.HasSuffix(r.URL.Path, "/labels/7") {
				t.Errorf("PATCH path: %q", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 7, "name": "defect", "color": "ff0000", "description": "renamed",
			})
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleLabelEdit, map[string]any{
		"repo": "o/r", "name": "bug", "rename": "defect", "description": "renamed",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&patchHits) != 1 {
		t.Errorf("expected 1 PATCH; got %d", patchHits)
	}
}

func TestLabelDeleteRequiresConfirm(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteHits, 1)
		}
		w.WriteHeader(204)
	})
	pinBuilder(t, p)

	// Without confirm: should be a preview, no DELETE.
	res, err := callTool(context.Background(), handleLabelDelete, map[string]any{
		"repo": "o/r", "name": "bug",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 0 {
		t.Errorf("preview must not DELETE; got %d", deleteHits)
	}
}

func TestLabelDeleteWithConfirm(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 7, "name": "bug", "color": "ff0000"},
			})
		case http.MethodDelete:
			atomic.AddInt32(&deleteHits, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleLabelDelete, map[string]any{
		"repo": "o/r", "name": "bug", "confirm": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 1 {
		t.Errorf("confirm=true must DELETE; got %d", deleteHits)
	}
}
