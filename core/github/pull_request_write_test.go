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

// TestMergePullRequestGHConflict verifies GitHub's 409 response surfaces
// as exitcode.MergeConflict so chains can yield on `merge_conflict`.
func TestMergePullRequestGHConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"message":"Head branch was modified"}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{})
	if err == nil {
		t.Fatal("expected error on 409")
	}
	if got := exitcode.Of(err); got != exitcode.MergeConflict {
		t.Errorf("409 → exit code: got %d, want MergeConflict (%d)", got, exitcode.MergeConflict)
	}
}

// TestMergePullRequestGHReviewRequired verifies GitHub's 405 with a
// review-related message surfaces as exitcode.ReviewRequired.
func TestMergePullRequestGHReviewRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(405)
		_, _ = w.Write([]byte(`{"message":"At least 1 approving review is required by reviewers with write access."}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{})
	if err == nil {
		t.Fatal("expected error on 405")
	}
	if got := exitcode.Of(err); got != exitcode.ReviewRequired {
		t.Errorf("405+review → exit code: got %d, want ReviewRequired (%d)", got, exitcode.ReviewRequired)
	}
}

// TestMergePullRequestGHPolicyViolation verifies GitHub's 405 with a
// non-review body (status check missing, locked branch, etc.) surfaces
// as exitcode.PolicyViolation.
func TestMergePullRequestGHPolicyViolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(405)
		_, _ = w.Write([]byte(`{"message":"Required status check 'ci/test' is expected."}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{})
	if err == nil {
		t.Fatal("expected error on 405")
	}
	if got := exitcode.Of(err); got != exitcode.PolicyViolation {
		t.Errorf("405+policy → exit code: got %d, want PolicyViolation (%d)", got, exitcode.PolicyViolation)
	}
}

// TestMergePullRequestGHIdempotentWhenAlreadyMerged: a policy 405 when
// the PR is already merged is idempotent success (#348).
func TestMergePullRequestGHIdempotentWhenAlreadyMerged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge"):
			w.WriteHeader(405)
			_, _ = w.Write([]byte(`{"message":""}`))
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "state": "closed", "merged": true})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{}); err != nil {
		t.Fatalf("already-merged PR: want nil (idempotent), got %v", err)
	}
}

// TestMergePullRequestGHNotMergedStaysError: a policy 405 where the PR
// is genuinely not merged still surfaces the policy error.
func TestMergePullRequestGHNotMergedStaysError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge"):
			w.WriteHeader(405)
			_, _ = w.Write([]byte(`{"message":""}`))
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "state": "open", "merged": false})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{})
	if err == nil {
		t.Fatal("not-merged PR with policy 405: want error, got nil")
	}
	if got := exitcode.Of(err); got != exitcode.PolicyViolation {
		t.Errorf("exit code: got %d, want PolicyViolation (%d)", got, exitcode.PolicyViolation)
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
