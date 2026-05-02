package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/github"
)

func TestWhoamiHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    1,
			"login": "octocat",
			"email": "leak@example.com",
		})
	}))
	defer srv.Close()

	p := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X"})
	got, err := p.Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if got != "octocat" {
		t.Errorf("login: %q", got)
	}
}

func TestWhoamiAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "BAD"})
	_, err := p.Whoami(context.Background())
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got %d, want Auth", got)
	}
}
