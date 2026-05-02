package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

func TestCreatePullRequestGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["title"] != "feat" || got["head"] != "feature/x" || got["base"] != "main" {
			t.Errorf("body: %+v", got)
		}
		_ = json.NewEncoder(w).Encode(makeGHPR(7, "open", false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.CreatePullRequest(context.Background(), "o", "r", provider.CreatePullRequestOptions{
		Title: "feat", Head: "feature/x", Base: "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if out.Number != 7 {
		t.Errorf("got %+v", out)
	}
}

func TestEditPullRequestGHDraft(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(makeGHPR(7, "open", false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	draft := true
	if _, err := p.EditPullRequest(context.Background(), "o", "r", 7, provider.EditPullRequestOptions{
		Draft: &draft,
	}); err != nil {
		t.Fatalf("EditPullRequest: %v", err)
	}
	if !strings.Contains(string(captured), `"draft":true`) {
		t.Errorf("body should send draft=true; got %s", captured)
	}
}

func TestMergePullRequestGH(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/merge") {
			t.Errorf("path: %q", r.URL.Path)
		}
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{
		Method:  "squash",
		Title:   "Merge PR #7",
		Message: "Notes here",
	}); err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(captured, &got)
	if got["merge_method"] != "squash" {
		t.Errorf("merge_method: %+v", got)
	}
	if got["commit_title"] != "Merge PR #7" {
		t.Errorf("commit_title: %+v", got)
	}
}

func TestMergePullRequestGHDefaults(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_ = p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{})
	if !strings.Contains(string(captured), `"merge_method":"merge"`) {
		t.Errorf("default method: %s", captured)
	}
}

func TestSubmitReviewGH(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/reviews") {
			t.Errorf("path: %q", r.URL.Path)
		}
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.SubmitReview(context.Background(), "o", "r", 7, provider.SubmitReviewOptions{
		Event: "APPROVED",
		Body:  "ship it",
		Comments: []provider.ReviewInlineComment{
			{Path: "x.go", Line: 10, Body: "rename"},
		},
	}); err != nil {
		t.Fatalf("SubmitReview: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(captured, &got)
	if got["event"] != "APPROVED" || got["body"] != "ship it" {
		t.Errorf("scalars: %+v", got)
	}
	comments, _ := got["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("comments count: %d", len(comments))
	}
	first := comments[0].(map[string]any)
	if first["path"] != "x.go" || first["position"].(float64) != 10 || first["body"] != "rename" {
		t.Errorf("inline (path/position/body): %+v", first)
	}
}

func TestSubmitReviewGHDefaultsToCommentEvent(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_ = p.SubmitReview(context.Background(), "o", "r", 7, provider.SubmitReviewOptions{Body: "fyi"})
	if !strings.Contains(string(captured), `"event":"COMMENT"`) {
		t.Errorf("default event: %s", captured)
	}
}
