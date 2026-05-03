// Cross-package mirror of cmd/gaia/main_test.go's
// TestGoldenScenarios. Lives in cli_test (same package as the rest
// of the CLI's unit tests) so that running `go test ./internal/cli`
// exercises the end-to-end harness — without -coverpkg, that's the
// only way coverage of the cli subcommands shows up against the
// internal/cli package itself rather than only against the
// cmd/gaia main package.
//
// The scenarios live in cmd/gaia/testdata/ so authors only maintain
// one set of fixtures. CLAUDE.md mandates that exact location for
// goldens; this file just walks the tree from a different package
// boundary.
//
// Mechanics mirror cmd/gaia/main_test.go: a tiny YAML scenario,
// optional in-process HTTP fake forge driven by a list of
// (method, path, status, body|body_file, content_type) rules, and
// a per-stage golden compare with the same normalize + token
// substitution rules.
//
// Update flow is the same:
//
//	go test ./cmd/gaia/... -run TestGoldenScenarios -update
//
// (The cmd/gaia harness is the canonical -update entry point. This
// file deliberately does NOT honor -update — there's only one
// source of truth for goldens, and it's cmd/gaia's harness.)
package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// scenarioYAML / stageYAML / mockRuleYAML / requestAssertionYAML
// mirror the shapes in cmd/gaia/main_test.go. Kept in this file
// rather than imported because cmd/gaia is `package main` and
// can't be imported by a test in another package — duplicating the
// types is the cleanest cross-cut.
type scenarioYAML struct {
	Description string         `yaml:"description,omitempty"`
	ChainYAML   string         `yaml:"chain_yaml,omitempty"`
	Mocks       []mockRuleYAML `yaml:"mocks,omitempty"`
	Stages      []stageYAML    `yaml:"stages"`
}

type mockRuleYAML struct {
	Method      string `yaml:"method"`
	Path        string `yaml:"path"`
	Query       string `yaml:"query,omitempty"`
	Status      int    `yaml:"status,omitempty"`
	Body        string `yaml:"body,omitempty"`
	BodyFile    string `yaml:"body_file,omitempty"`
	ContentType string `yaml:"content_type,omitempty"`
}

type stageYAML struct {
	Args          []string              `yaml:"args"`
	Env           map[string]string     `yaml:"env,omitempty"`
	Stdin         string                `yaml:"stdin,omitempty"`
	ExitCode      int                   `yaml:"exit_code,omitempty"`
	Golden        string                `yaml:"golden,omitempty"`
	AssertRequest *requestAssertionYAML `yaml:"assert_request,omitempty"`
	Description   string                `yaml:"description,omitempty"`
}

type requestAssertionYAML struct {
	Method        string `yaml:"method,omitempty"`
	Path          string `yaml:"path,omitempty"`
	Query         string `yaml:"query,omitempty"`
	BodyContains  string `yaml:"body_contains,omitempty"`
	BodyEquals    string `yaml:"body_equals,omitempty"`
	HeaderName    string `yaml:"header_name,omitempty"`
	HeaderEquals  string `yaml:"header_equals,omitempty"`
	HeaderHasAuth bool   `yaml:"header_has_auth,omitempty"`
}

// TestGoldenScenariosFromCLI walks cmd/gaia/testdata/<command>/...
// from inside the cli_test package. Only scenarios that touch the
// in-process gaia CLI surface are exercised — chain scenarios that
// rely on shell exec are skipped here (the cmd/gaia harness covers
// those; running them from this package is pure noise).
func TestGoldenScenariosFromCLI(t *testing.T) {
	root := findTestdataRoot(t)
	commands, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmdDir := range commands {
		if !cmdDir.IsDir() {
			continue
		}
		// Skip chain — those scenarios drive `gaia chain run` which
		// shells out to `echo` / `false`; the cmd/gaia harness
		// covers them and re-running here doubles the cost without
		// adding signal.
		if cmdDir.Name() == "chain" {
			continue
		}
		group := filepath.Join(root, cmdDir.Name())
		entries, err := os.ReadDir(group)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := cmdDir.Name() + "/" + e.Name()
			scenarioDir := filepath.Join(group, e.Name())
			t.Run(name, func(t *testing.T) {
				runCLIScenario(t, scenarioDir)
			})
		}
	}
}

// findTestdataRoot resolves the cmd/gaia/testdata/ directory from
// internal/cli's working directory. Works when run via
// `go test ./internal/cli/...` (cwd = internal/cli) and from the
// repo root.
func findTestdataRoot(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../cmd/gaia/testdata", // run from internal/cli
		"cmd/gaia/testdata",       // run from repo root
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			abs, err := filepath.Abs(p)
			if err != nil {
				t.Fatal(err)
			}
			return abs
		}
	}
	t.Fatal("cmd/gaia/testdata not found from cwd")
	return ""
}

func runCLIScenario(t *testing.T, dir string) {
	t.Helper()
	scenarioFile := filepath.Join(dir, "scenario.yaml")
	raw, err := os.ReadFile(scenarioFile)
	if err != nil {
		t.Fatalf("read scenario.yaml: %v", err)
	}
	var sc scenarioYAML
	if err := yaml.Unmarshal(raw, &sc); err != nil {
		t.Fatalf("parse scenario.yaml: %v", err)
	}

	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	chainFile := filepath.Join(tempDir, "chain.yaml")

	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "config"))
	t.Setenv("XDG_STATE_HOME", stateDir)
	for _, k := range []string{
		"FORGEJO_TOKEN", "FORGEJO_API_URL",
		"GITEA_TOKEN", "GITHUB_TOKEN", "GH_TOKEN",
		"GAIA_PROFILE", "GAIA_PROVIDER",
	} {
		t.Setenv(k, "")
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	notGit := t.TempDir()
	if err := os.Chdir(notGit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var (
		forge  *cliFakeForge
		apiURL string
	)
	if len(sc.Mocks) > 0 {
		forge = newCLIFakeForge(t, dir, sc.Mocks)
		apiURL = forge.URL()
		t.Cleanup(forge.Close)
	}

	stageOuts := make([]string, len(sc.Stages))
	for i, st := range sc.Stages {
		stageOuts[i] = runCLIStage(t, dir, i, st, sc, tempDir, stateDir, chainFile, apiURL, forge, stageOuts)
	}
}

func runCLIStage(
	t *testing.T,
	scenarioDir string,
	index int,
	st stageYAML,
	_ scenarioYAML,
	tempDir, stateDir, chainFile, apiURL string,
	forge *cliFakeForge,
	prior []string,
) string {
	t.Helper()
	stageName := fmt.Sprintf("stage-%d", index)
	t.Run(stageName, func(t *testing.T) {
		subst := func(s string) string {
			s = strings.ReplaceAll(s, "${tempdir}", tempDir)
			s = strings.ReplaceAll(s, "${chainfile}", chainFile)
			s = strings.ReplaceAll(s, "${stateDir}", stateDir)
			s = strings.ReplaceAll(s, "${scenarioDir}", scenarioDir)
			if apiURL != "" {
				s = strings.ReplaceAll(s, "${apiURL}", apiURL)
			}
			for j := 0; j < index; j++ {
				placeholder := fmt.Sprintf("${token:%d}", j)
				if !strings.Contains(s, placeholder) {
					continue
				}
				tok := mineCLIResumeToken(prior[j])
				if tok == "" {
					t.Fatalf("stage %d references ${token:%d} but stage-%d had no resume_token", index, j, j)
				}
				s = strings.ReplaceAll(s, placeholder, tok)
			}
			return s
		}
		args := make([]string, len(st.Args))
		for i, a := range st.Args {
			args[i] = subst(a)
		}
		for k, v := range st.Env {
			t.Setenv(k, subst(v))
		}

		var stdout, stderr bytes.Buffer
		root := cli.NewRootCmd()
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(args)
		if st.Stdin != "" {
			root.SetIn(strings.NewReader(st.Stdin))
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		execErr := root.ExecuteContext(ctx)
		gotExit := exitcode.Of(execErr)
		if gotExit != st.ExitCode {
			t.Errorf("exit code: got %d want %d\nstderr: %s\nstdout: %s",
				gotExit, st.ExitCode, stderr.String(), stdout.String())
		}

		if st.AssertRequest != nil && forge != nil {
			assertCLIRequest(t, forge, st.AssertRequest)
		}

		goldenName := st.Golden
		if goldenName == "" {
			goldenName = stageName + ".golden"
		}
		goldenPath := filepath.Join(scenarioDir, goldenName)

		got := normalizeCLI(stdout.String(), tempDir, stateDir, chainFile)
		if apiURL != "" {
			got = strings.ReplaceAll(got, apiURL, "<APIURL>")
		}
		got = strings.ReplaceAll(got, scenarioDir, "<SCENARIODIR>")

		wantBytes, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("read golden %s: %v\n--- got ---\n%s", goldenPath, err, got)
		}
		want := string(wantBytes)
		if got != want {
			t.Errorf("golden mismatch (%s)\n--- got ---\n%s--- want ---\n%s",
				goldenPath, got, want)
		}
		prior[index] = stdout.String()
	})
	return prior[index]
}

func mineCLIResumeToken(stdout string) string {
	var env struct {
		Data struct {
			ResumeToken string `json:"resume_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		return ""
	}
	return env.Data.ResumeToken
}

func normalizeCLI(s, tempDir, stateDir, chainFile string) string {
	if chainFile != "" {
		s = strings.ReplaceAll(s, chainFile, "<CHAINFILE>")
	}
	if stateDir != "" {
		s = strings.ReplaceAll(s, stateDir, "<STATEDIR>")
	}
	if tempDir != "" {
		s = strings.ReplaceAll(s, tempDir, "<TEMPDIR>")
	}
	s = cliDurationRE.ReplaceAllString(s, `"duration_ms": 0`)
	s = cliResumeTokenRE.ReplaceAllString(s, `"resume_token": "<TOKEN>"`)
	s = cliListTokenRE.ReplaceAllString(s, `"token": "<TOKEN>"`)
	s = cliModTimeRE.ReplaceAllString(s, `"mod_time": "<MODTIME>"`)
	return s
}

var (
	cliDurationRE    = regexp.MustCompile(`"duration_ms":\s*\d+`)
	cliResumeTokenRE = regexp.MustCompile(`"resume_token":\s*"[0-9a-f]{32}"`)
	cliListTokenRE   = regexp.MustCompile(`"token":\s*"[0-9a-f]{32}"`)
	cliModTimeRE     = regexp.MustCompile(`"mod_time":\s*"[^"]+"`)
)

// cliFakeForge mirrors cmd/gaia/main_test.go's fakeForge.
type cliFakeForge struct {
	srv      *httptest.Server
	rules    []mockRuleYAML
	scenario string
	mu       sync.Mutex
	requests []cliRecordedRequest
}

type cliRecordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   []byte
	Header http.Header
}

func newCLIFakeForge(t *testing.T, scenarioDir string, rules []mockRuleYAML) *cliFakeForge {
	t.Helper()
	f := &cliFakeForge{rules: rules, scenario: scenarioDir}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *cliFakeForge) URL() string { return f.srv.URL }
func (f *cliFakeForge) Close()      { f.srv.Close() }

func (f *cliFakeForge) LastRequest() cliRecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return cliRecordedRequest{}
	}
	return f.requests[len(f.requests)-1]
}

func (f *cliFakeForge) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	f.mu.Lock()
	f.requests = append(f.requests, cliRecordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Body:   body,
		Header: r.Header.Clone(),
	})
	f.mu.Unlock()
	for _, rule := range f.rules {
		if !cliRuleMatches(rule, r) {
			continue
		}
		ct := rule.ContentType
		if ct == "" {
			ct = "application/json"
		}
		w.Header().Set("Content-Type", ct)
		status := rule.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		respBody, err := f.ruleBody(rule)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(respBody)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = fmt.Fprintf(w, `{"message":"no mock for %s %s"}`, r.Method, r.URL.Path)
}

func cliRuleMatches(rule mockRuleYAML, r *http.Request) bool {
	if rule.Method != "" && !strings.EqualFold(rule.Method, r.Method) {
		return false
	}
	if rule.Path != r.URL.Path {
		return false
	}
	if rule.Query != "" && rule.Query != r.URL.RawQuery {
		return false
	}
	return true
}

func (f *cliFakeForge) ruleBody(rule mockRuleYAML) ([]byte, error) {
	if rule.Body != "" {
		return []byte(rule.Body), nil
	}
	if rule.BodyFile != "" {
		p := filepath.Join(f.scenario, rule.BodyFile)
		return os.ReadFile(p)
	}
	return nil, nil
}

func assertCLIRequest(t *testing.T, f *cliFakeForge, a *requestAssertionYAML) {
	t.Helper()
	got := f.LastRequest()
	if a.Method != "" && !strings.EqualFold(got.Method, a.Method) {
		t.Errorf("assert_request.method: got %q want %q", got.Method, a.Method)
	}
	if a.Path != "" && got.Path != a.Path {
		t.Errorf("assert_request.path: got %q want %q", got.Path, a.Path)
	}
	if a.Query != "" && got.Query != a.Query {
		t.Errorf("assert_request.query: got %q want %q", got.Query, a.Query)
	}
	if a.BodyContains != "" && !strings.Contains(string(got.Body), a.BodyContains) {
		t.Errorf("assert_request.body_contains %q not in body: %s", a.BodyContains, string(got.Body))
	}
	if a.BodyEquals != "" && strings.TrimSpace(string(got.Body)) != strings.TrimSpace(a.BodyEquals) {
		t.Errorf("assert_request.body_equals: got %q want %q", string(got.Body), a.BodyEquals)
	}
	if a.HeaderName != "" {
		if h := got.Header.Get(a.HeaderName); h != a.HeaderEquals {
			t.Errorf("assert_request.header[%s]: got %q want %q", a.HeaderName, h, a.HeaderEquals)
		}
	}
	if a.HeaderHasAuth {
		if got.Header.Get("Authorization") == "" {
			t.Errorf("assert_request.header_has_auth: Authorization header missing; got headers %v", got.Header)
		}
	}
}
