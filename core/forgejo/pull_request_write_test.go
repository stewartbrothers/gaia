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

func TestCreatePullRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls" {
			t.Errorf("path: %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["title"] != "feat" || got["head"] != "feature/x" || got["base"] != "main" {
			t.Errorf("body: %+v", got)
		}
		_ = json.NewEncoder(w).Encode(makePR(7, "feat", "open", nil))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.CreatePullRequest(context.Background(), "o", "r", provider.CreatePullRequestOptions{
		Title: "feat",
		Head:  "feature/x",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if out.Number != 7 {
		t.Errorf("number: %d", out.Number)
	}
}

func TestEditPullRequestState(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: %q", r.Method)
		}
		capturedBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(makePR(7, "feat", "closed", nil))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.EditPullRequest(context.Background(), "o", "r", 7, provider.EditPullRequestOptions{
		State: "closed",
	})
	if err != nil {
		t.Fatalf("EditPullRequest: %v", err)
	}
	if !strings.Contains(string(capturedBody), `"state":"closed"`) {
		t.Errorf("body: %s", capturedBody)
	}
}

func TestEditPullRequestDraftFlag(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(makePR(7, "feat", "open", nil))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	draft := true
	_, err := p.EditPullRequest(context.Background(), "o", "r", 7, provider.EditPullRequestOptions{Draft: &draft})
	if err != nil {
		t.Fatalf("EditPullRequest: %v", err)
	}
	if !strings.Contains(string(capturedBody), `"draft":true`) {
		t.Errorf("body should send draft=true; got %s", capturedBody)
	}
}

func TestMergePullRequestSendsMethod(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/merge") {
			t.Errorf("path: %q", r.URL.Path)
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{
		Method: "squash",
	}); err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	if !strings.Contains(string(capturedBody), `"do":"squash"`) {
		t.Errorf("body should carry do=squash; got %s", capturedBody)
	}
}

func TestMergePullRequestDefaultsToMerge(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_ = p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{})
	if !strings.Contains(string(capturedBody), `"do":"merge"`) {
		t.Errorf("default method should be merge; got %s", capturedBody)
	}
}

func TestMergePullRequestNotMergeable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(405)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.MergePullRequest(context.Background(), "o", "r", 7, provider.MergePullRequestOptions{})
	if err == nil {
		t.Fatal("expected error on 405 (not mergeable)")
	}
	// 405 isn't in the FromHTTP map; defaults to Generic.
	if got := exitcode.Of(err); got != exitcode.Generic {
		t.Errorf("405 → exit code: got %d", got)
	}
}
