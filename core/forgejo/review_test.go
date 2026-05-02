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

func TestSubmitReviewPostsAllFields(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/42/reviews" {
			t.Errorf("path: %q", r.URL.Path)
		}
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.SubmitReview(context.Background(), "o", "r", 42, provider.SubmitReviewOptions{
		Event: "APPROVED",
		Body:  "ship it",
		Comments: []provider.ReviewInlineComment{
			{Path: "foo.go", Line: 10, Body: "rename this"},
			{Path: "bar.go", Line: 20, Body: "tighten loop"},
		},
	})
	if err != nil {
		t.Fatalf("SubmitReview: %v", err)
	}

	var got map[string]any
	_ = json.Unmarshal(captured, &got)
	if got["event"] != "APPROVED" || got["body"] != "ship it" {
		t.Errorf("scalars: %+v", got)
	}
	comments, ok := got["comments"].([]any)
	if !ok || len(comments) != 2 {
		t.Fatalf("comments: %+v", got["comments"])
	}
	first := comments[0].(map[string]any)
	if first["path"] != "foo.go" || first["new_position"].(float64) != 10 || first["body"] != "rename this" {
		t.Errorf("first inline: %+v", first)
	}
}

func TestSubmitReviewDefaultsToCommentEvent(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_ = p.SubmitReview(context.Background(), "o", "r", 42, provider.SubmitReviewOptions{Body: "fyi"})
	if !strings.Contains(string(captured), `"event":"COMMENT"`) {
		t.Errorf("default event should be COMMENT; got %s", captured)
	}
}

func TestSubmitReviewNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.SubmitReview(context.Background(), "o", "r", 999, provider.SubmitReviewOptions{Event: "APPROVED"})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}
