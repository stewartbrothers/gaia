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

// SubmitReview happy path + default-event are covered in
// pull_request_write_test.go. The tests here cover the differences
// from the Forgejo provider that callers depend on (no new_position
// field on the wire) and the error paths the parity matrix promises.

func TestSubmitReviewGHEmitsPositionNotNewPosition(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.SubmitReview(context.Background(), "o", "r", 1, provider.SubmitReviewOptions{
		Event:    "REQUEST_CHANGES",
		Comments: []provider.ReviewInlineComment{{Path: "x.go", Line: 5, Body: "no"}},
	}); err != nil {
		t.Fatalf("SubmitReview: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(captured, &got)
	first := got["comments"].([]any)[0].(map[string]any)
	if _, leaked := first["new_position"]; leaked {
		t.Errorf("github wire shape must use position, not new_position: %+v", first)
	}
	if first["position"].(float64) != 5 {
		t.Errorf("position: %+v", first)
	}
}

func TestSubmitReviewGHOmitsEmptyComments(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.SubmitReview(context.Background(), "o", "r", 1, provider.SubmitReviewOptions{
		Event: "APPROVED",
	}); err != nil {
		t.Fatalf("SubmitReview: %v", err)
	}
	if strings.Contains(string(captured), `"comments"`) {
		t.Errorf("empty comments must be omitted; got %s", captured)
	}
}

func TestSubmitReviewGHNotFound(t *testing.T) {
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

func TestSubmitReviewGHAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.SubmitReview(context.Background(), "o", "r", 1, provider.SubmitReviewOptions{Event: "APPROVED"})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got %d, want Auth", got)
	}
}
