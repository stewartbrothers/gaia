package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/github"
)

func fastClient(t *testing.T, baseURL, token string) *github.Client {
	t.Helper()
	return github.New(github.Options{
		BaseURL:   baseURL,
		Token:     token,
		UserAgent: "gaia-test/1.0",
		RetryWait: 1 * time.Millisecond,
	})
}

func TestGetSendsBearerAuthAndAPIVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer TEST" {
			t.Errorf("Authorization: got %q, want Bearer TEST", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept: got %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("X-GitHub-Api-Version: got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "TEST")
	var out struct {
		OK string `json:"ok"`
	}
	if err := c.Get(context.Background(), "/x", &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out.OK != "yes" {
		t.Errorf("decode: got %+v", out)
	}
}

func TestGetMapsHTTPStatusToExitCode(t *testing.T) {
	cases := []struct {
		status int
		want   int
	}{
		{401, exitcode.Auth},
		{403, exitcode.Auth},
		{404, exitcode.NotFound},
		{422, exitcode.Usage},
		{429, exitcode.RateLimit},
	}
	for _, c := range cases {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
			}))
			defer srv.Close()

			cl := fastClient(t, srv.URL, "X")
			err := cl.Get(context.Background(), "/x", nil)
			if got := exitcode.Of(err); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestGet5xxRetriesOnce(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "X")
	if err := c.Get(context.Background(), "/x", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected 2 calls; got %d", calls)
	}
}

func TestPostSendsJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"title":"hello"`) {
			t.Errorf("body: %s", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 1})
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "TEST")
	if err := c.Post(context.Background(), "/issues", map[string]string{"title": "hello"}, nil); err != nil {
		t.Fatalf("Post: %v", err)
	}
}

func TestGetRawWithCustomAccept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github.v3.diff" {
			t.Errorf("Accept: got %q", got)
		}
		_, _ = w.Write([]byte("diff --git a/x b/x"))
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "TEST")
	body, err := c.GetRaw(context.Background(), "/x", "application/vnd.github.v3.diff")
	if err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if !strings.HasPrefix(string(body), "diff --git") {
		t.Errorf("body: %s", body)
	}
}

func TestNewDefaultsToProductionAPI(t *testing.T) {
	c := github.New(github.Options{Token: "X"})
	if c == nil {
		t.Fatal("nil client")
	}
}

func TestTokenNeverInError(t *testing.T) {
	const secret = "very-secret-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, secret)
	err := c.Get(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("token leaked: %q", err.Error())
	}
}
