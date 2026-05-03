package cli_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// wikiMeta returns the on-the-wire shape Forgejo's GET /wiki/pages
// emits per page (no body).
func wikiMeta(title, sha string) map[string]any {
	return map[string]any{
		"title":   title,
		"sub_url": title,
		"last_commit": map[string]any{
			"sha": sha,
			"commiter": map[string]any{
				"name": "alice", "email": "alice@example",
				"date": "2026-04-01T00:00:00Z",
			},
		},
	}
}

// wikiFull returns the full GET /wiki/page/{slug} shape with body
// base64-encoded as Forgejo does.
func wikiFull(title, body, sha string) map[string]any {
	return map[string]any{
		"title":          title,
		"sub_url":        title,
		"content_base64": base64.StdEncoding.EncodeToString([]byte(body)),
		"last_commit": map[string]any{
			"sha": sha,
			"commiter": map[string]any{
				"name": "alice", "email": "alice@example",
				"date": "2026-05-03T00:00:00Z",
			},
		},
	}
}

func TestWikiList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/wiki/pages" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			wikiMeta("Home", "deadbeef1234567"),
			wikiMeta("Setup", "cafebabe9876543"),
		})
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "list",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	// WikiPage.Title carries the trust-external tag (#146).
	var env struct {
		Data []struct {
			Title      trustExternal `json:"title"`
			Path       string        `json:"path"`
			LastCommit string        `json:"last_commit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data) != 2 || env.Data[0].Title.Value != "Home" {
		t.Errorf("data: %+v", env.Data)
	}
	if env.Data[0].LastCommit != "deadbee" {
		t.Errorf("last_commit short SHA expected; got %q", env.Data[0].LastCommit)
	}
}

func TestWikiView(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/wiki/page/Home" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(wikiFull("Home", "# Welcome\n\nThis is the home page.", "deadbeef1234567"))
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "view", "Home",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	// WikiPage.Title and WikiPage.Body both carry the trust-external
	// tag (#146).
	var env struct {
		Data struct {
			Title trustExternal `json:"title"`
			Path  string        `json:"path"`
			Body  trustExternal `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if env.Data.Title.Value != "Home" || !strings.Contains(env.Data.Body.Value, "Welcome") {
		t.Errorf("data: %+v", env.Data)
	}
	if env.Data.Title.Trust != "external" || env.Data.Body.Trust != "external" {
		t.Errorf("trust marker missing: %+v", env.Data)
	}
}

func TestWikiViewNotFoundIsExitCode3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "view", "Missing",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got code %d, want NotFound", got)
	}
}

func TestWikiSearchReturnsHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/wiki/pages":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				wikiMeta("Home", "sha1234567"),
				wikiMeta("Setup", "sha2345678"),
			})
		case r.URL.Path == "/repos/o/r/wiki/page/Home":
			_ = json.NewEncoder(w).Encode(wikiFull("Home", "Welcome to the project. Body has FOO sprinkled in.", "sha1234567"))
		case r.URL.Path == "/repos/o/r/wiki/page/Setup":
			_ = json.NewEncoder(w).Encode(wikiFull("Setup", "FOO is the magic config knob.", "sha2345678"))
		}
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "search", "FOO",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	// WikiSearchHit.Title and WikiSearchHit.Snippet carry the
	// trust-external tag (#146).
	var env struct {
		Data []struct {
			Path    string        `json:"path"`
			Title   trustExternal `json:"title"`
			Snippet trustExternal `json:"snippet"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data) != 2 {
		t.Errorf("hit count: %d (data=%+v)", len(env.Data), env.Data)
	}
	for _, h := range env.Data {
		if h.Snippet.Value == "" || !strings.Contains(h.Snippet.Value, "FOO") {
			t.Errorf("snippet should contain match; got %+v", h.Snippet)
		}
	}
}

func TestWikiEditCreatesIfMissing(t *testing.T) {
	posts := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			w.WriteHeader(404) // page does not exist yet
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/wiki/new":
			atomic.AddInt32(&posts, 1)
			body, _ := io.ReadAll(r.Body)
			var got map[string]any
			_ = json.Unmarshal(body, &got)
			decoded, _ := base64.StdEncoding.DecodeString(got["content_base64"].(string))
			if string(decoded) != "new body" {
				t.Errorf("body: %q", decoded)
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(wikiFull("Home", "new body", "newsha1234567"))
		}
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "edit", "Home",
		"--body", "new body",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if atomic.LoadInt32(&posts) != 1 {
		t.Errorf("expected 1 POST; got %d", posts)
	}
}

func TestWikiEditReadsStdin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(404)
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var got map[string]any
			_ = json.Unmarshal(body, &got)
			decoded, _ := base64.StdEncoding.DecodeString(got["content_base64"].(string))
			if string(decoded) != "from stdin" {
				t.Errorf("body: %q", decoded)
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(wikiFull("Home", "from stdin", "newsha1234567"))
		}
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("from stdin"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "edit", "Home",
		"--body", "-",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
}

func TestWikiEditDryRunDoesNotWrite(t *testing.T) {
	calls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "edit", "Home",
		"--body", "preview only",
		"--dry-run",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("--dry-run must not contact the forge; got %d calls", calls)
	}
	if !strings.Contains(stdout.String(), "PUT (upsert)") {
		t.Errorf("dry-run should print method+path label; got %q", stdout.String())
	}
}

func TestWikiEditRequiresBody(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", "http://x",
		"--repo", "o/r",
		"wiki", "edit", "Home",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --body missing")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("got code %d, want Usage", got)
	}
}

func TestWikiDeletePreviewWithoutConfirm(t *testing.T) {
	calls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "delete", "Home",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("delete without --confirm must not call API; got %d calls", calls)
	}
	if !strings.Contains(stdout.String(), "Would delete") {
		t.Errorf("preview message missing; got %q", stdout.String())
	}
}

func TestWikiDeleteWithConfirm(t *testing.T) {
	deleteCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %q", r.Method)
		}
		atomic.AddInt32(&deleteCalls, 1)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"wiki", "delete", "Home",
		"--confirm",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if atomic.LoadInt32(&deleteCalls) != 1 {
		t.Errorf("expected 1 DELETE; got %d", deleteCalls)
	}
	if !strings.Contains(stdout.String(), "Deleted") {
		t.Errorf("success message missing; got %q", stdout.String())
	}
}
