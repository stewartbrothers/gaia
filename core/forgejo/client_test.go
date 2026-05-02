package forgejo_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
)

// fastClient returns a Client wired to baseURL with retries set to a
// near-zero backoff so the suite stays under a second.
func fastClient(t *testing.T, baseURL, token string) *forgejo.Client {
	t.Helper()
	return forgejo.New(forgejo.Options{
		BaseURL:   baseURL,
		Token:     token,
		UserAgent: "gaia-test/1.0",
		RetryWait: 1 * time.Millisecond,
	})
}

func TestGetSuccessDecodesJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token TEST" {
			t.Errorf("Authorization: got %q, want %q", got, "token TEST")
		}
		if got := r.Header.Get("User-Agent"); got != "gaia-test/1.0" {
			t.Errorf("User-Agent: got %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept: got %q", got)
		}
		if r.URL.Path != "/api/v1/version" {
			t.Errorf("path: got %q, want /api/v1/version", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "8.0.3"})
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL+"/api/v1", "TEST")
	var out struct {
		Version string `json:"version"`
	}
	if err := c.Get(context.Background(), "/version", &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out.Version != "8.0.3" {
		t.Errorf("decoded version: got %q, want 8.0.3", out.Version)
	}
}

func TestGetPathLeadingSlashOptional(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			hit = true
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "X")
	// Either form must reach the same path.
	if err := c.Get(context.Background(), "version", nil); err != nil {
		t.Fatalf("Get without slash: %v", err)
	}
	if !hit {
		t.Errorf("expected /version to be hit")
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
				_, _ = w.Write([]byte(`{"message":"x"}`))
			}))
			defer srv.Close()

			cl := fastClient(t, srv.URL, "X")
			err := cl.Get(context.Background(), "/x", nil)
			if err == nil {
				t.Fatal("expected error on non-2xx; got nil")
			}
			if got := exitcode.Of(err); got != c.want {
				t.Errorf("exit code: got %d, want %d", got, c.want)
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
	var out map[string]string
	if err := c.Get(context.Background(), "/x", &out); err != nil {
		t.Fatalf("Get after retry: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 calls (initial + 1 retry), got %d", got)
	}
	if out["ok"] != "yes" {
		t.Errorf("decoded body: got %+v", out)
	}
}

func TestGet5xxRetryGivesUpAfterOne(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(502)
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "X")
	err := c.Get(context.Background(), "/x", nil)
	if got := exitcode.Of(err); got != exitcode.Network {
		t.Errorf("exit code: got %d, want Network", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected exactly 2 calls (initial + 1 retry), got %d", got)
	}
}

func TestGet4xxNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "X")
	_ = c.Get(context.Background(), "/x", nil)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("4xx should not retry; got %d calls", got)
	}
}

func TestGetContextCancelDoesNotRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c := forgejo.New(forgejo.Options{
		BaseURL:   srv.URL,
		Token:     "X",
		RetryWait: 1 * time.Hour, // long enough that ctx cancel beats it
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled
	err := c.Get(ctx, "/x", nil)
	if err == nil {
		t.Fatal("expected error from canceled context; got nil")
	}
}

func TestGetIncludesPathInErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "X")
	err := c.Get(context.Background(), "/repos/foo/bar/issues/42", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "/repos/foo/bar/issues/42") {
		t.Errorf("error should mention path; got %q", err.Error())
	}
}

func TestGetTokenNeverInError(t *testing.T) {
	const secret = "super-secret-do-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"unauthenticated"}`))
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, secret)
	err := c.Get(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("token must never appear in error; got %q", err.Error())
	}
}

func TestNetworkErrorMapsToNetworkExitCode(t *testing.T) {
	c := forgejo.New(forgejo.Options{
		BaseURL:    "http://127.0.0.1:1", // reserved port; refuses connect
		Token:      "X",
		RetryWait:  1 * time.Millisecond,
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
	})
	err := c.Get(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("expected network error")
	}
	if got := exitcode.Of(err); got != exitcode.Network {
		t.Errorf("dial failure: got code %d, want Network", got)
	}
}

func TestGetWithEmptyTokenSkipsAuthHeader(t *testing.T) {
	// `gaia version` doesn't need auth; an empty token must produce a
	// request without an Authorization header rather than a literal
	// `Authorization: token ` (which some servers reject).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header should be absent for empty token; got %q", got)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := forgejo.New(forgejo.Options{
		BaseURL:   srv.URL,
		Token:     "",
		RetryWait: 1 * time.Millisecond,
	})
	if err := c.Get(context.Background(), "/x", nil); err != nil {
		t.Fatalf("Get with empty token: %v", err)
	}
}

func TestPostSendsJSONBodyAndDecodesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type: got %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["title"] != "hello" {
			t.Errorf("body.title: got %v", got["title"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 42})
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "TEST")
	var out struct {
		Number int `json:"number"`
	}
	if err := c.Post(context.Background(), "/issues", map[string]string{"title": "hello"}, &out); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if out.Number != 42 {
		t.Errorf("decoded: got %d, want 42", out.Number)
	}
}

func TestPostNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"message":"validation failed"}`))
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "TEST")
	err := c.Post(context.Background(), "/issues", map[string]string{}, nil)
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("exit code: got %d, want Usage(2)", got)
	}
}

func TestPatchSendsJSONBody(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: got %q, want PATCH", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 7})
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "TEST")
	if err := c.Patch(context.Background(), "/issues/7", map[string]string{"state": "closed"}, nil); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if !strings.Contains(captured, `"state":"closed"`) {
		t.Errorf("body: got %q", captured)
	}
}

func TestDelete204IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: got %q, want DELETE", r.Method)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "TEST")
	if err := c.Delete(context.Background(), "/labels/bug"); err != nil {
		t.Errorf("Delete 204: got %v, want nil", err)
	}
}

func TestDeleteNotFoundIsExitCodeNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := fastClient(t, srv.URL, "TEST")
	err := c.Delete(context.Background(), "/labels/missing")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound(3)", got)
	}
}

func TestNewDefaultsApply(t *testing.T) {
	c := forgejo.New(forgejo.Options{BaseURL: "https://example", Token: "X"})
	if c == nil {
		t.Fatal("New returned nil")
	}
	// Defaults are exercised indirectly elsewhere; here we just assert
	// New doesn't panic on a minimal Options.
	var _ error = errors.New("placeholder, keeps the import live")
}
