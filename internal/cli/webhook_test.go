package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

func cliHookJSON(id int64, url, ct string, events []string, active bool) map[string]any {
	return map[string]any{
		"id": id,
		"config": map[string]any{
			"url":          url,
			"content_type": ct,
		},
		"events":     events,
		"active":     active,
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-01T00:00:00Z",
	}
}

func runGaia(t *testing.T, srvURL string, args ...string) (string, string, error) {
	t.Helper()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	allArgs := []string{
		"--provider", "forgejo",
		"--api-url", srvURL,
		"--repo", "o/r",
	}
	allArgs = append(allArgs, args...)
	root.SetArgs(allArgs)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestWebhookListCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			cliHookJSON(1, "https://example.com/h1", "json", []string{"push"}, true),
		})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "webhook", "list")
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	var env struct {
		Data []struct {
			ID  int64  `json:"id"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if len(env.Data) != 1 || env.Data[0].URL != "https://example.com/h1" {
		t.Errorf("got %+v", env.Data)
	}
}

func TestWebhookViewCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/hooks/42") {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(cliHookJSON(42, "https://example.com/x", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "webhook", "view", "42")
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, `"url": "https://example.com/x"`) {
		t.Errorf("expected url in output; got %q", stdout)
	}
}

func TestWebhookCreateCLI(t *testing.T) {
	captured := []byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(cliHookJSON(7, "https://example.com/wh", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	_, stderr, err := runGaia(t, srv.URL,
		"webhook", "create",
		"--url", "https://example.com/wh",
		"--content-type", "json",
		"--secret", "shh",
		"--events", "push,pull_request",
		"--active",
	)
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(string(captured), `"url":"https://example.com/wh"`) {
		t.Errorf("body url: %s", captured)
	}
	if !strings.Contains(string(captured), `"secret":"shh"`) {
		t.Errorf("body secret: %s", captured)
	}
}

func TestWebhookCreateRequiresURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	_, _, err := runGaia(t, srv.URL,
		"webhook", "create",
		"--content-type", "json",
		"--events", "push",
	)
	if err == nil {
		t.Fatal("expected error when --url missing")
	}
}

func TestWebhookCreateBadContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	_, _, err := runGaia(t, srv.URL,
		"webhook", "create",
		"--url", "https://x",
		"--content-type", "xml",
		"--events", "push",
	)
	if err == nil {
		t.Fatal("expected error for content-type=xml")
	}
}

func TestWebhookCreateDryRunRedactsSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("dry-run must not contact API")
	}))
	defer srv.Close()
	stdout, _, err := runGaia(t, srv.URL,
		"webhook", "create",
		"--url", "https://x",
		"--content-type", "json",
		"--events", "push",
		"--secret", "supersecret",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(stdout, "supersecret") {
		t.Errorf("dry-run leaked secret: %s", stdout)
	}
	// json.Encoder escapes < and > by default; either form indicates the
	// redaction worked.
	if !strings.Contains(stdout, "redacted") {
		t.Errorf("expected redacted marker in dry-run output: %s", stdout)
	}
}

func TestWebhookEditCLI(t *testing.T) {
	patchHits := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(cliHookJSON(7, "https://old", "json", []string{"push"}, true))
		case http.MethodPatch:
			atomic.AddInt32(&patchHits, 1)
			_ = json.NewEncoder(w).Encode(cliHookJSON(7, "https://new", "json", []string{"push", "issues"}, true))
		}
	}))
	defer srv.Close()
	_, stderr, err := runGaia(t, srv.URL,
		"webhook", "edit", "7",
		"--url", "https://new",
		"--add-events", "issues",
	)
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	if atomic.LoadInt32(&patchHits) != 1 {
		t.Errorf("expected 1 PATCH; got %d", patchHits)
	}
}

func TestWebhookEditMutuallyExclusiveActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	_, _, err := runGaia(t, srv.URL,
		"webhook", "edit", "7",
		"--active", "--inactive",
	)
	if err == nil {
		t.Fatal("expected error when both --active and --inactive set")
	}
}

func TestWebhookDeleteRequiresConfirm(t *testing.T) {
	calls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	stdout, _, err := runGaia(t, srv.URL, "webhook", "delete", "7")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("preview must not call API; got %d calls", calls)
	}
	if !strings.Contains(stdout, "Would delete") {
		t.Errorf("stdout: %q", stdout)
	}
}

func TestWebhookDeleteWithConfirm(t *testing.T) {
	deleteCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteCalls, 1)
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()
	_, stderr, err := runGaia(t, srv.URL, "webhook", "delete", "7", "--confirm")
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	if atomic.LoadInt32(&deleteCalls) != 1 {
		t.Errorf("expected 1 DELETE; got %d", deleteCalls)
	}
}

func TestWebhookDeliveriesList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 101, "event": "push", "status": 200, "response_status": 200, "duration": 1.2, "delivered_at": "2026-04-01T00:00:00Z"},
		})
	}))
	defer srv.Close()
	stdout, stderr, err := runGaia(t, srv.URL, "webhook", "deliveries", "7")
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	var env struct {
		Data []struct {
			ID    int64  `json:"id"`
			Event string `json:"event"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if len(env.Data) != 1 || env.Data[0].Event != "push" {
		t.Errorf("got %+v", env.Data)
	}
}

func TestWebhookDeliveriesGetOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/deliveries/101") {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":              101,
			"event":           "push",
			"status":          200,
			"response_status": 200,
			"duration":        1.0,
			"delivered_at":    "2026-04-01T00:00:00Z",
			"request_body":    `{"ref":"refs/heads/main"}`,
		})
	}))
	defer srv.Close()
	stdout, stderr, err := runGaia(t, srv.URL, "webhook", "deliveries", "7", "--get", "101")
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "request_body") {
		t.Errorf("expected request_body in detail output: %s", stdout)
	}
}

func TestWebhookRedeliver(t *testing.T) {
	hits := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/deliveries/101") {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()
	_, stderr, err := runGaia(t, srv.URL, "webhook", "redeliver", "7", "101")
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("expected 1 POST; got %d", hits)
	}
}

func TestWebhookTest(t *testing.T) {
	hits := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/hooks/7/tests") {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()
	_, stderr, err := runGaia(t, srv.URL, "webhook", "test", "7")
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("expected 1 POST; got %d", hits)
	}
}
