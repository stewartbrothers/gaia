package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func deliveryJSON(id int64, event string, status int, duration float64, redelivery bool) map[string]any {
	return map[string]any{
		"id":              id,
		"event":           event,
		"action":          "opened",
		"status":          status,
		"response_status": status,
		"duration":        duration,
		"is_redelivery":   redelivery,
		"delivered":       "2026-04-01T00:00:00Z",
		"delivered_at":    "2026-04-01T00:00:00Z",
	}
}

func TestListWebhookDeliveries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks/7/deliveries" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			deliveryJSON(101, "push", 200, 1.5, false),
			deliveryJSON(102, "pull_request", 500, 0.25, true),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListWebhookDeliveries(context.Background(), "o", "r", 7, provider.ListDeliveriesOptions{})
	if err != nil {
		t.Fatalf("ListWebhookDeliveries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].ID != 101 || got[0].Event != "push" || got[0].StatusCode != 200 {
		t.Errorf("first delivery: %+v", got[0])
	}
	// duration 1.5s should round to 1500ms
	if got[0].DurationMs != 1500 {
		t.Errorf("duration_ms: got %d, want 1500", got[0].DurationMs)
	}
	if !got[1].Redelivery {
		t.Errorf("redelivery flag should round-trip; got false on second")
	}
}

func TestListWebhookDeliveriesAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListWebhookDeliveries(context.Background(), "o", "r", 7, provider.ListDeliveriesOptions{})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got %d, want Auth", got)
	}
}

func TestGetWebhookDelivery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks/7/deliveries/101" {
			t.Errorf("path: %q", r.URL.Path)
		}
		full := deliveryJSON(101, "push", 200, 1.5, false)
		full["request_headers"] = map[string]any{"Content-Type": "application/json"}
		full["request_body"] = `{"ref":"refs/heads/main"}`
		full["response_headers"] = map[string]any{"Content-Type": "text/plain"}
		full["response_body"] = "ok"
		_ = json.NewEncoder(w).Encode(full)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetWebhookDelivery(context.Background(), "o", "r", 7, 101)
	if err != nil {
		t.Fatalf("GetWebhookDelivery: %v", err)
	}
	if got.ID != 101 || got.RequestBody == "" || got.ResponseBody != "ok" {
		t.Errorf("detail not populated: %+v", got)
	}
	if got.RequestHeaders["Content-Type"] != "application/json" {
		t.Errorf("request_headers: %+v", got.RequestHeaders)
	}
}

func TestRedeliverWebhook(t *testing.T) {
	hits := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/hooks/7/deliveries/101" {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.RedeliverWebhook(context.Background(), "o", "r", 7, 101); err != nil {
		t.Fatalf("RedeliverWebhook: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("expected 1 POST; got %d", hits)
	}
}

func TestRedeliverWebhookNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.RedeliverWebhook(context.Background(), "o", "r", 7, 101)
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}

func TestTestWebhook(t *testing.T) {
	hits := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/hooks/7/tests" {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.TestWebhook(context.Background(), "o", "r", 7); err != nil {
		t.Fatalf("TestWebhook: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("expected 1 POST; got %d", hits)
	}
}

func TestTestWebhookNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.TestWebhook(context.Background(), "o", "r", 7)
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}
