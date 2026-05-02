package forgejo_test

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

func TestCreateIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q", r.Method)
		}
		if r.URL.Path != "/repos/o/r/issues" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["title"] != "hello" || got["body"] != "world" {
			t.Errorf("body: %+v", got)
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
	if out.Number != 42 || out.Title != "hello" {
		t.Errorf("got %+v", out)
	}
}

func TestEditIssueState(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: got %q", r.Method)
		}
		if r.URL.Path != "/repos/o/r/issues/42" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(makeIssue(42, "hello", "closed"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.EditIssue(context.Background(), "o", "r", 42, provider.EditIssueOptions{
		State: "closed",
	})
	if err != nil {
		t.Fatalf("EditIssue: %v", err)
	}
	if out.State != "closed" {
		t.Errorf("state: %q", out.State)
	}
	if captured["state"] != "closed" {
		t.Errorf("body should send state=closed; got %+v", captured)
	}
	if _, has := captured["title"]; has {
		t.Errorf("empty Title should be omitted; got %+v", captured)
	}
}

func TestEditIssueOmitsEmptyFields(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(makeIssue(1, "x", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.EditIssue(context.Background(), "o", "r", 1, provider.EditIssueOptions{
		Title: "new title",
	})
	if err != nil {
		t.Fatalf("EditIssue: %v", err)
	}
	if captured["title"] != "new title" {
		t.Errorf("title not sent: %+v", captured)
	}
	for _, banned := range []string{"body", "state", "assignees"} {
		if _, has := captured[banned]; has {
			t.Errorf("empty %s should be omitted; got %+v", banned, captured)
		}
	}
}

func TestCreateIssueComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/42/comments" {
			t.Errorf("path: %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["body"] != "lgtm" {
			t.Errorf("body: %+v", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         99,
			"user":       map[string]any{"login": "alice"},
			"body":       "lgtm",
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
	if out.ID != 99 || out.Body != "lgtm" || out.Source != "issue" {
		t.Errorf("got %+v", out)
	}
}

func TestEditIssueComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: %q", r.Method)
		}
		if r.URL.Path != "/repos/o/r/issues/comments/99" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         99,
			"user":       map[string]any{"login": "alice"},
			"body":       "edited",
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
		t.Errorf("body: %q", out.Body)
	}
}

func TestDeleteIssueComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %q", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/issues/comments/99") {
			t.Errorf("path: %q", r.URL.Path)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteIssueComment(context.Background(), "o", "r", 99); err != nil {
		t.Errorf("DeleteIssueComment: %v", err)
	}
}

func TestDeleteIssueCommentNotFound(t *testing.T) {
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
