package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func TestCreateIssueGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"title":"hello"`) {
			t.Errorf("body: %s", body)
		}
		_ = json.NewEncoder(w).Encode(makeIssue(42, "hello", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.CreateIssue(context.Background(), "o", "r", provider.CreateIssueOptions{
		Title:  "hello",
		Body:   "world",
		Labels: []string{"bug"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if out.Number != 42 {
		t.Errorf("got %+v", out)
	}
}

func TestEditIssueGHState(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: %q", r.Method)
		}
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(makeIssue(42, "x", "closed"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.EditIssue(context.Background(), "o", "r", 42, provider.EditIssueOptions{State: "closed"}); err != nil {
		t.Fatalf("EditIssue: %v", err)
	}
	if !strings.Contains(string(captured), `"state":"closed"`) {
		t.Errorf("body: %s", captured)
	}
	if strings.Contains(string(captured), `"title"`) {
		t.Errorf("empty title should be omitted; got %s", captured)
	}
}

func TestCreateIssueCommentGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/42/comments" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 99, "user": map[string]any{"login": "alice"}, "body": "lgtm",
			"created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.CreateIssueComment(context.Background(), "o", "r", 42, "lgtm")
	if err != nil {
		t.Fatalf("CreateIssueComment: %v", err)
	}
	if out.ID != 99 || out.Source != "issue" || out.Body != "lgtm" {
		t.Errorf("got %+v", out)
	}
}

func TestEditIssueCommentGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: %q", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 99, "user": map[string]any{"login": "a"}, "body": "edited",
			"created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-01T00:05:00Z",
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.EditIssueComment(context.Background(), "o", "r", 99, "edited")
	if err != nil {
		t.Fatalf("EditIssueComment: %v", err)
	}
	if out.Body != "edited" {
		t.Errorf("got %+v", out)
	}
}

func TestDeleteIssueCommentGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %q", r.Method)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteIssueComment(context.Background(), "o", "r", 99); err != nil {
		t.Errorf("DeleteIssueComment: %v", err)
	}
}

func TestDeleteIssueCommentGHNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeleteIssueComment(context.Background(), "o", "r", 99)
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}
