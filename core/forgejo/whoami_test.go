package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
)

func TestWhoamiHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("path: got %q, want /user", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token TEST" {
			t.Errorf("Authorization: got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         1,
			"login":      "Gerwood",
			"full_name":  "Gerwood Stewart",
			"email":      "leak@example.com",
			"avatar_url": "https://x/y.png",
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if got != "Gerwood" {
		t.Errorf("login: got %q, want Gerwood", got)
	}
}

func TestWhoami401MapsToAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"unauthenticated"}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.Whoami(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth", got)
	}
}

func TestWhoamiNetworkErrorMapsToNetwork(t *testing.T) {
	p := forgejo.NewProvider(forgejo.Options{
		BaseURL:    "http://127.0.0.1:1",
		Token:      "X",
		RetryWait:  1 * time.Millisecond,
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
	})
	_, err := p.Whoami(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
	if got := exitcode.Of(err); got != exitcode.Network {
		t.Errorf("exit code: got %d, want Network", got)
	}
}

func TestWhoamiTokenNeverInError(t *testing.T) {
	const secret = "secret-token-do-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := forgejo.NewProvider(forgejo.Options{
		BaseURL:   srv.URL,
		Token:     secret,
		RetryWait: 1 * time.Millisecond,
	})
	_, err := p.Whoami(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("token leaked into error: %q", err.Error())
	}
}
