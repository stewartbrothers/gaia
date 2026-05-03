package forgejo_test

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

// hookJSON returns a Forgejo-shaped webhook record for httptest.
// The shape mirrors what `GET /repos/{o}/{r}/hooks/{id}` returns.
func hookJSON(id int64, url, ct string, events []string, active bool) map[string]any {
	return map[string]any{
		"id":   id,
		"type": "gitea",
		"config": map[string]any{
			"url":          url,
			"content_type": ct,
		},
		"events":     events,
		"active":     active,
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-01T01:00:00Z",
	}
}

func TestListWebhooks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			hookJSON(1, "https://example.com/h1", "json", []string{"push", "pull_request"}, true),
			hookJSON(2, "https://example.com/h2", "form", []string{"push"}, false),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListWebhooks(context.Background(), "o", "r", provider.ListWebhooksOptions{})
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].URL != "https://example.com/h1" || got[0].ContentType != "json" {
		t.Errorf("first webhook: %+v", got[0])
	}
	if len(got[0].Events) != 2 || got[0].Events[0] != "push" {
		t.Errorf("events not preserved: %+v", got[0].Events)
	}
	if got[1].Active {
		t.Errorf("active flag should round-trip; got true for hook 2")
	}
}

func TestGetWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks/42" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(hookJSON(42, "https://example.com/x", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetWebhook(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if got.ID != 42 || got.URL != "https://example.com/x" {
		t.Errorf("got %+v", got)
	}
}

func TestGetWebhookNotFound(t *testing.T) {
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

func TestCreateWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		if r.URL.Path != "/repos/o/r/hooks" {
			t.Errorf("path: %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"type":"gitea"`) {
			t.Errorf("body must have type:gitea; got %s", body)
		}
		if !strings.Contains(string(body), `"url":"https://example.com/wh"`) {
			t.Errorf("body must have config.url; got %s", body)
		}
		if !strings.Contains(string(body), `"content_type":"json"`) {
			t.Errorf("body must have config.content_type; got %s", body)
		}
		if !strings.Contains(string(body), `"secret":"shh"`) {
			t.Errorf("body must include secret on create; got %s", body)
		}
		_ = json.NewEncoder(w).Encode(hookJSON(7, "https://example.com/wh", "json", []string{"push"}, true))
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

func TestCreateWebhookFormContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"content_type":"form"`) {
			t.Errorf("body must round-trip form content type; got %s", body)
		}
		_ = json.NewEncoder(w).Encode(hookJSON(8, "https://example.com/wh", "form", []string{"push"}, true))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.CreateWebhook(context.Background(), "o", "r", provider.CreateWebhookOptions{
		URL:         "https://example.com/wh",
		ContentType: "form",
		Events:      []string{"push"},
		Active:      true,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
}

func TestCreateWebhookAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.CreateWebhook(context.Background(), "o", "r", provider.CreateWebhookOptions{
		URL:         "https://example.com/wh",
		ContentType: "json",
		Events:      []string{"push"},
	})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got %d, want Auth", got)
	}
}

func TestEditWebhookAddRemoveEventsMerge(t *testing.T) {
	getCalls := int32(0)
	patchBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			atomic.AddInt32(&getCalls, 1)
			_ = json.NewEncoder(w).Encode(hookJSON(7, "https://example.com/wh", "json", []string{"push", "issues"}, true))
		case http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			patchBody = string(body)
			_ = json.NewEncoder(w).Encode(hookJSON(7, "https://example.com/wh", "json", []string{"push", "pull_request"}, true))
		}
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
	if atomic.LoadInt32(&getCalls) != 1 {
		t.Errorf("expected 1 GET to fetch existing events; got %d", getCalls)
	}
	// PATCH body must include merged events: push (kept) + pull_request (added);
	// issues removed.
	if !strings.Contains(patchBody, `"push"`) || !strings.Contains(patchBody, `"pull_request"`) {
		t.Errorf("merged event list missing; PATCH body: %s", patchBody)
	}
	if strings.Contains(patchBody, `"issues"`) {
		t.Errorf("removed event should not be in PATCH body: %s", patchBody)
	}
}

func TestEditWebhookActiveFlip(t *testing.T) {
	patchBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(hookJSON(7, "https://example.com/wh", "json", []string{"push"}, true))
		case http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			patchBody = string(body)
			_ = json.NewEncoder(w).Encode(hookJSON(7, "https://example.com/wh", "json", []string{"push"}, false))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	f := false
	_, err := p.EditWebhook(context.Background(), "o", "r", 7, provider.EditWebhookOptions{
		Active: &f,
	})
	if err != nil {
		t.Fatalf("EditWebhook: %v", err)
	}
	if !strings.Contains(patchBody, `"active":false`) {
		t.Errorf("active=false should be in PATCH body: %s", patchBody)
	}
}

func TestEditWebhookURLAndSecret(t *testing.T) {
	patchBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(hookJSON(7, "https://old.example.com", "json", []string{"push"}, true))
		case http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			patchBody = string(body)
			_ = json.NewEncoder(w).Encode(hookJSON(7, "https://new.example.com", "json", []string{"push"}, true))
		}
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
		t.Errorf("url change missing from PATCH body: %s", patchBody)
	}
	if !strings.Contains(patchBody, `"secret":"rotated"`) {
		t.Errorf("secret rotation missing from PATCH body: %s", patchBody)
	}
}

func TestDeleteWebhook(t *testing.T) {
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

func TestDeleteWebhookNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeleteWebhook(context.Background(), "o", "r", 999)
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}
