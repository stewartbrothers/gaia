package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
)

func mcpHookJSON(id int64, url, ct string, events []string, active bool) map[string]any {
	return map[string]any{
		"id": id,
		"config": map[string]any{
			"url":          url,
			"content_type": ct,
		},
		"events":     events,
		"active":     active,
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-01T00:00:00Z",
	}
}

func TestWebhookListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			mcpHookJSON(1, "https://example.com/h1", "json", []string{"push"}, true),
			mcpHookJSON(2, "https://example.com/h2", "form", []string{"push"}, false),
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookList, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 2 {
		t.Errorf("count: %d", len(arr))
	}
}

func TestWebhookViewToolRequiresID(t *testing.T) {
	res, _ := callTool(context.Background(), handleWebhookView, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing id must error")
	}
}

func TestWebhookViewTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks/42" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(mcpHookJSON(42, "https://example.com/x", "json", []string{"push"}, true))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookView, map[string]any{
		"repo": "o/r", "id": float64(42),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestWebhookCreateTool(t *testing.T) {
	postHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&postHits, 1)
		}
		_ = json.NewEncoder(w).Encode(mcpHookJSON(99, "https://example.com/wh", "json", []string{"push"}, true))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookCreate, map[string]any{
		"repo":         "o/r",
		"url":          "https://example.com/wh",
		"content_type": "json",
		"secret":       "shh",
		"events":       []any{"push", "pull_request"},
		"active":       true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&postHits) != 1 {
		t.Errorf("expected 1 POST; got %d", postHits)
	}
}

func TestWebhookCreateRequiresURL(t *testing.T) {
	res, _ := callTool(context.Background(), handleWebhookCreate, map[string]any{
		"repo": "o/r", "content_type": "json", "events": []any{"push"},
	})
	if !res.IsError {
		t.Error("missing url must error")
	}
}

func TestWebhookCreateRejectsBadContentType(t *testing.T) {
	res, _ := callTool(context.Background(), handleWebhookCreate, map[string]any{
		"repo": "o/r", "url": "https://x", "content_type": "xml", "events": []any{"push"},
	})
	if !res.IsError {
		t.Error("bad content_type must error")
	}
}

func TestWebhookEditTool(t *testing.T) {
	patchHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(mcpHookJSON(7, "https://old", "json", []string{"push"}, true))
		case http.MethodPatch:
			atomic.AddInt32(&patchHits, 1)
			_ = json.NewEncoder(w).Encode(mcpHookJSON(7, "https://new", "json", []string{"push", "issues"}, true))
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookEdit, map[string]any{
		"repo": "o/r", "id": float64(7),
		"url":        "https://new",
		"add_events": []any{"issues"},
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&patchHits) != 1 {
		t.Errorf("expected 1 PATCH; got %d", patchHits)
	}
}

func TestWebhookDeletePreview(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(_ http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteHits, 1)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookDelete, map[string]any{
		"repo": "o/r", "id": float64(7),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 0 {
		t.Errorf("preview must not DELETE; got %d", deleteHits)
	}
}

func TestWebhookDeleteWithConfirm(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteHits, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookDelete, map[string]any{
		"repo": "o/r", "id": float64(7), "confirm": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 1 {
		t.Errorf("confirm=true must DELETE; got %d", deleteHits)
	}
}

func TestWebhookDeliveriesListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 101, "event": "push", "status": 200, "response_status": 200, "duration": 1.0, "delivered_at": "2026-04-01T00:00:00Z"},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookDeliveries, map[string]any{
		"repo": "o/r", "id": float64(7),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Errorf("count: %d", len(arr))
	}
}

func TestWebhookDeliveriesGetOneTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":              101,
			"event":           "push",
			"status":          200,
			"response_status": 200,
			"duration":        1.0,
			"delivered_at":    "2026-04-01T00:00:00Z",
			"request_body":    `{"x":1}`,
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookDeliveries, map[string]any{
		"repo": "o/r", "id": float64(7), "delivery_id": float64(101),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestWebhookRedeliverTool(t *testing.T) {
	hits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookRedeliver, map[string]any{
		"repo": "o/r", "id": float64(7), "delivery_id": float64(101),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("expected 1 POST; got %d", hits)
	}
}

func TestWebhookTestTool(t *testing.T) {
	hits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWebhookTest, map[string]any{
		"repo": "o/r", "id": float64(7),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("expected 1 POST; got %d", hits)
	}
}
