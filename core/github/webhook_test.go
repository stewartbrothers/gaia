package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func ghHookJSON(id int64, url, ct string, events []string, active bool) map[string]any {
	return map[string]any{
		"id":   id,
		"name": "web",
		"config": map[string]any{
			"url":          url,
			"content_type": ct,
			"insecure_ssl": "0",
		},
		"events":     events,
		"active":     active,
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-01T01:00:00Z",
	}
}

func TestGHListWebhooks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			ghHookJSON(1, "https://example.com/h1", "json", []string{"push"}, true),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListWebhooks(context.Background(), "o", "r", provider.ListWebhooksOptions{})
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].URL != "https://example.com/h1" || got[0].ContentType != "json" {
		t.Errorf("got %+v", got[0])
	}
}

func TestGHGetWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks/42" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ghHookJSON(42, "https://example.com/x", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetWebhook(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if got.URL != "https://example.com/x" {
		t.Errorf("got %+v", got)
	}
}

func TestGHGetWebhookNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	p := newTestProvider(t, srv.URL)
	_, err := p.GetWebhook(context.Background(), "o", "r", 999)
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}

func TestGHCreateWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"name":"web"`) {
			t.Errorf("name=web required in body: %s", body)
		}
		if !strings.Contains(string(body), `"url":"https://example.com/wh"`) {
			t.Errorf("config.url missing: %s", body)
		}
		if !strings.Contains(string(body), `"secret":"shh"`) {
			t.Errorf("secret should be in body: %s", body)
		}
		_ = json.NewEncoder(w).Encode(ghHookJSON(7, "https://example.com/wh", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.CreateWebhook(context.Background(), "o", "r", provider.CreateWebhookOptions{
		URL:         "https://example.com/wh",
		ContentType: "json",
		Secret:      "shh",
		Events:      []string{"push"},
		Active:      true,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if got.ID != 7 {
		t.Errorf("got id %d", got.ID)
	}
}

func TestGHCreateWebhookAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	p := newTestProvider(t, srv.URL)
	_, err := p.CreateWebhook(context.Background(), "o", "r", provider.CreateWebhookOptions{
		URL:         "x",
		ContentType: "json",
		Events:      []string{"push"},
	})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got %d, want Auth", got)
	}
}

func TestGHEditWebhookAddRemoveEvents(t *testing.T) {
	patchBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			body, _ := io.ReadAll(r.Body)
			patchBody = string(body)
		}
		_ = json.NewEncoder(w).Encode(ghHookJSON(7, "https://example.com/wh", "json", []string{"push", "pull_request"}, true))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.EditWebhook(context.Background(), "o", "r", 7, provider.EditWebhookOptions{
		AddEvents:    []string{"pull_request"},
		RemoveEvents: []string{"issues"},
	})
	if err != nil {
		t.Fatalf("EditWebhook: %v", err)
	}
	// GitHub accepts add_events / remove_events directly — no merge dance.
	if !strings.Contains(patchBody, `"add_events"`) {
		t.Errorf("add_events missing from PATCH body: %s", patchBody)
	}
	if !strings.Contains(patchBody, `"remove_events"`) {
		t.Errorf("remove_events missing from PATCH body: %s", patchBody)
	}
}

func TestGHEditWebhookActiveFlip(t *testing.T) {
	patchBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			body, _ := io.ReadAll(r.Body)
			patchBody = string(body)
		}
		_ = json.NewEncoder(w).Encode(ghHookJSON(7, "https://example.com/wh", "json", []string{"push"}, false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	f := false
	_, err := p.EditWebhook(context.Background(), "o", "r", 7, provider.EditWebhookOptions{Active: &f})
	if err != nil {
		t.Fatalf("EditWebhook: %v", err)
	}
	if !strings.Contains(patchBody, `"active":false`) {
		t.Errorf("active=false missing from PATCH body: %s", patchBody)
	}
}

func TestGHEditWebhookURLAndSecret(t *testing.T) {
	patchBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			body, _ := io.ReadAll(r.Body)
			patchBody = string(body)
		}
		_ = json.NewEncoder(w).Encode(ghHookJSON(7, "https://new.example.com", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.EditWebhook(context.Background(), "o", "r", 7, provider.EditWebhookOptions{
		URL:    "https://new.example.com",
		Secret: "rotated",
	})
	if err != nil {
		t.Fatalf("EditWebhook: %v", err)
	}
	if !strings.Contains(patchBody, `"url":"https://new.example.com"`) {
		t.Errorf("url missing: %s", patchBody)
	}
	if !strings.Contains(patchBody, `"secret":"rotated"`) {
		t.Errorf("secret missing: %s", patchBody)
	}
}

func TestGHDeleteWebhook(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletePath = r.URL.Path
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()
	p := newTestProvider(t, srv.URL)
	if err := p.DeleteWebhook(context.Background(), "o", "r", 7); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if deletePath != "/repos/o/r/hooks/7" {
		t.Errorf("DELETE path: got %q", deletePath)
	}
}

func ghDeliveryJSON(id int64, event string, status int, duration float64, redelivery bool) map[string]any {
	return map[string]any{
		"id":           id,
		"event":        event,
		"action":       "opened",
		"status":       "OK",
		"status_code":  status,
		"duration":     duration,
		"redelivery":   redelivery,
		"delivered_at": "2026-04-01T00:00:00Z",
	}
}

func TestGHListWebhookDeliveries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks/7/deliveries" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			ghDeliveryJSON(101, "push", 200, 1.5, false),
			ghDeliveryJSON(102, "pull_request", 500, 0.25, true),
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
	if got[0].StatusCode != 200 {
		t.Errorf("status_code not promoted: %+v", got[0])
	}
	if got[0].DurationMs != 1500 {
		t.Errorf("duration_ms: got %d, want 1500", got[0].DurationMs)
	}
	if !got[1].Redelivery {
		t.Errorf("redelivery flag not preserved")
	}
}

func TestGHGetWebhookDelivery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks/7/deliveries/101" {
			t.Errorf("path: %q", r.URL.Path)
		}
		full := ghDeliveryJSON(101, "push", 200, 1.5, false)
		full["request"] = map[string]any{
			"headers": map[string]any{"Content-Type": "application/json"},
			"payload": `{"ref":"refs/heads/main"}`,
		}
		full["response"] = map[string]any{
			"headers": map[string]any{"Content-Type": "text/plain"},
			"payload": "ok",
		}
		_ = json.NewEncoder(w).Encode(full)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetWebhookDelivery(context.Background(), "o", "r", 7, 101)
	if err != nil {
		t.Fatalf("GetWebhookDelivery: %v", err)
	}
	if got.ResponseBody != "ok" {
		t.Errorf("response body: %q", got.ResponseBody)
	}
	if got.RequestHeaders["Content-Type"] != "application/json" {
		t.Errorf("request headers: %+v", got.RequestHeaders)
	}
}

func TestGHRedeliverWebhook(t *testing.T) {
	hits := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/hooks/7/deliveries/101/attempts" {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(202)
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

func TestGHTestWebhook(t *testing.T) {
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
