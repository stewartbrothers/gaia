package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

const fakeDiffMixed = `diff --git a/a.txt b/a.txt
index 111..222 100644
--- a/a.txt
+++ b/a.txt
@@ -1,1 +1,2 @@
 keep
+added
diff --git a/binary.png b/binary.png
index abc..def 100644
Binary files a/binary.png and b/binary.png differ
`

func TestPRDiffJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/42.diff" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(fakeDiffMixed))
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
		"pr", "diff", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	var env struct {
		Data []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
			Binary bool   `json:"binary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data) != 2 {
		t.Fatalf("file count: got %d", len(env.Data))
	}
	if env.Data[1].Path != "binary.png" || !env.Data[1].Binary {
		t.Errorf("binary file: got %+v", env.Data[1])
	}
}

func TestPRDiffPathsFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fakeDiffMixed))
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
		"pr", "diff", "42",
		"--paths", "a.txt",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var env struct {
		Data []struct {
			Path string `json:"path"`
		} `json:"data"`
	}
	_ = json.Unmarshal(stdout.Bytes(), &env)
	if len(env.Data) != 1 || env.Data[0].Path != "a.txt" {
		t.Errorf("paths filter: got %+v", env.Data)
	}
}

func TestPRDiffPretty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fakeDiffMixed))
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
		"--format", "pretty",
		"pr", "diff", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"=== a.txt [modified] ===", "+added", "=== binary.png [modified] ===", "(binary)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q; got %q", want, out)
		}
	}
}
