// Package main's test harness — golden-file driver for `gaia` CLI
// subcommands.
//
// CLAUDE.md mandates "end-to-end tests live in cmd/gaia/testdata/ as
// golden files driven by an in-process fake forge server. New
// subcommand → new golden file." This file is the harness; testdata/
// holds the per-scenario fixtures.
//
// Layout:
//
//	cmd/gaia/testdata/
//	  chain/
//	    run-success/
//	      scenario.yaml      — multi-stage test description
//	      stage-0.golden     — expected stdout for stage 0
//	    run-yield-then-list/
//	      scenario.yaml      — yield, then list, then abort
//	      stage-0.golden     — yield envelope
//	      stage-1.golden     — list output (one entry)
//	      stage-2.golden     — abort envelope
//
// Why "stages": some scenarios (yield→resume, run→list) need state
// to persist across multiple gaia invocations. Stages share an
// XDG_STATE_HOME tempdir, so the second stage finds what the first
// stage wrote. Resume tokens captured from stage N's stdout are
// re-injected into stage N+1's args via the `${token:N}` substitution.
//
// Why in-process: gaia is a Go module; cli.NewRootCmd() returns the
// same command tree main() invokes. Driving it directly keeps each
// scenario sub-100ms and lines failure messages up with the source.
//
// Update flow:
//
//	go test ./cmd/gaia/... -run TestGoldenScenarios -update
//	git diff cmd/gaia/testdata
//
// Always inspect before committing. -update is a write-everything
// trapdoor — never CI's default.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
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

// updateGolden, when -update is passed to `go test`, rewrites every
// .golden file under testdata/ rather than comparing.
var updateGolden = flag.Bool("update", false, "update .golden files in cmd/gaia/testdata/ instead of asserting")

// scenario describes a multi-stage end-to-end test against the
// in-process gaia CLI. Stages run in order, sharing an
// XDG_STATE_HOME so chain run / list / resume / abort can be
// composed in one fixture.
type scenario struct {
	// Description: docs-only. A future operator scanning testdata/
	// can read intent without reverse-engineering args.
	Description string `yaml:"description,omitempty"`

	// ChainYAML, when non-empty, is written to ${chainfile} before
	// any stage runs. Stages reference the file via the
	// ${chainfile} token in their args.
	ChainYAML string `yaml:"chain_yaml,omitempty"`

	// SavedChains, when non-empty, is written to
	// ${tempdir}/.config/gaia/chains/<name>.yaml before stages run.
	// Lets a scenario exercise saved-chain resolution
	// (`gaia chain run <name>`) without mocking the filesystem.
	// Phase B-3 / #112.
	SavedChains map[string]string `yaml:"saved_chains,omitempty"`

	// Mocks, when non-empty, drives an in-process HTTP fake forge
	// server. The harness starts an httptest.NewServer that
	// dispatches requests to the first matching rule (by
	// method+path, in order). Stages reference the server via the
	// ${apiURL} token in their args. Empty means no fake forge is
	// started (chain scenarios don't need one).
	Mocks []mockRule `yaml:"mocks,omitempty"`

	// Files, when non-empty, writes arbitrary fixture files into
	// the scenario tempdir before any stage runs. Map key is the
	// relative path under ${tempdir}; value is the file body.
	// Parent directories are created automatically with mode 0700.
	// File mode defaults to 0600; override per-file via a leading
	// `#!mode:0644\n` header on the body (consumed by the writer;
	// not written to the file). Used by `gaia config doctor`
	// scenarios to seed `${tempdir}/config/gaia/config.yaml` etc.
	// without expanding the harness DSL further.
	Files map[string]string `yaml:"files,omitempty"`

	// Stages run sequentially. Each one produces a golden file
	// named "stage-N.golden" by default (override per-stage with
	// stage.golden).
	Stages []stage `yaml:"stages"`
}

// mockRule is one entry in the fake forge's dispatch table. Rules
// are matched in order; the first method+path match wins. The body
// is taken from `body` (inline JSON/text) or `body_file` (path
// relative to the scenario directory) — `body_file` lets large
// fixtures live in their own file without bloating scenario.yaml.
//
// `query` is an optional exact-match on the URL's RawQuery, useful
// when a list-style endpoint is exercised twice with different
// filter combinations.
type mockRule struct {
	Method   string `yaml:"method"`
	Path     string `yaml:"path"`
	Query    string `yaml:"query,omitempty"`
	Status   int    `yaml:"status,omitempty"`
	Body     string `yaml:"body,omitempty"`
	BodyFile string `yaml:"body_file,omitempty"`
	// ContentType overrides the default `application/json`. Mostly
	// used by diff fixtures that ship `text/plain`.
	ContentType string `yaml:"content_type,omitempty"`
}

// stage is one gaia invocation within a scenario.
//
// Argv tokens supported:
//
//	${tempdir}     — scenario tempdir (HOME also points here).
//	${chainfile}   — path to the written ChainYAML.
//	${stateDir}    — XDG_STATE_HOME for the scenario.
//	${scenarioDir} — absolute path to the scenario's testdata
//	                 directory (so fixture files committed alongside
//	                 scenario.yaml can be referenced as
//	                 ${scenarioDir}/assets/foo.tar.gz).
//	${apiURL}      — base URL of the in-process fake forge server
//	                 (only present when scenario.Mocks is set).
//	${token:N}    — resume_token mined from stage N's stdout
//	                envelope. Lets a "resume" stage reference
//	                what a prior "run" stage yielded.
type stage struct {
	Args     []string          `yaml:"args"`
	Env      map[string]string `yaml:"env,omitempty"`
	Stdin    string            `yaml:"stdin,omitempty"`
	ExitCode int               `yaml:"exit_code,omitempty"`
	Golden   string            `yaml:"golden,omitempty"`

	// AssertRequest, when set, captures the most recently
	// dispatched fake-forge request and asserts on it. method/path
	// must match exactly; body_contains is a substring check on
	// the recorded request body.
	AssertRequest *requestAssertion `yaml:"assert_request,omitempty"`

	// Description: docs-only.
	Description string `yaml:"description,omitempty"`
}

// requestAssertion describes what a stage expects the fake forge to
// have received as the most-recent request. Substring assertions
// (body_contains) keep goldens stable across map-iteration order in
// JSON bodies; exact assertions (method/path/query) catch routing
// regressions.
type requestAssertion struct {
	Method        string `yaml:"method,omitempty"`
	Path          string `yaml:"path,omitempty"`
	Query         string `yaml:"query,omitempty"`
	BodyContains  string `yaml:"body_contains,omitempty"`
	BodyEquals    string `yaml:"body_equals,omitempty"`
	HeaderName    string `yaml:"header_name,omitempty"`
	HeaderEquals  string `yaml:"header_equals,omitempty"`
	HeaderHasAuth bool   `yaml:"header_has_auth,omitempty"`
}

// TestGoldenScenarios walks testdata/, loading each scenario.yaml
// and running it through the in-process gaia CLI. Failures show the
// per-stage diff so the operator can tell at a glance what changed.
func TestGoldenScenarios(t *testing.T) {
	root := "testdata"
	commands, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no testdata directory yet")
		}
		t.Fatal(err)
	}
	for _, cmdDir := range commands {
		if !cmdDir.IsDir() {
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
				runScenario(t, scenarioDir)
			})
		}
	}
}

// runScenario executes one scenario through every stage, asserting
// per-stage golden output along the way. The scenario tempdir +
// state dir are shared across stages so chain state persists.
func runScenario(t *testing.T, dir string) {
	t.Helper()

	scenarioFile := filepath.Join(dir, "scenario.yaml")
	raw, err := os.ReadFile(scenarioFile)
	if err != nil {
		t.Fatalf("read scenario.yaml: %v", err)
	}
	var sc scenario
	if err := yaml.Unmarshal(raw, &sc); err != nil {
		t.Fatalf("parse scenario.yaml: %v", err)
	}

	// Resolve scenarioDir to absolute before any os.Chdir; the
	// harness chdirs to a non-git tempdir and golden paths must
	// keep working.
	absScenarioDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}

	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	chainFile := filepath.Join(tempDir, "chain.yaml")
	if sc.ChainYAML != "" {
		if err := os.WriteFile(chainFile, []byte(sc.ChainYAML), 0o600); err != nil {
			t.Fatalf("write chain yaml: %v", err)
		}
	}
	// Saved chains land at the global location
	// (XDG_CONFIG_HOME/gaia/chains/<name>.yaml — XDG_CONFIG_HOME is
	// pinned to ${tempdir}/config below). The chain CLI also probes
	// project-local .gaia/chains/ but the harness chdirs to a non-
	// git tempdir so that layer is silently skipped — exactly the
	// "global fallback" path. Phase B-3 / #112.
	if len(sc.SavedChains) > 0 {
		// chainResolveOptions in internal/cli/chain.go uses
		// os.UserHomeDir() for the global lookup, which honors HOME
		// (already pinned to tempDir below). So the layout we need
		// is ${HOME}/.config/gaia/chains/<name>.yaml.
		savedDir := filepath.Join(tempDir, ".config", "gaia", "chains")
		if err := os.MkdirAll(savedDir, 0o700); err != nil {
			t.Fatalf("mkdir saved chains: %v", err)
		}
		for name, body := range sc.SavedChains {
			fname := name
			if !strings.HasSuffix(fname, ".yaml") && !strings.HasSuffix(fname, ".yml") {
				fname += ".yaml"
			}
			if err := os.WriteFile(filepath.Join(savedDir, fname), []byte(body), 0o600); err != nil {
				t.Fatalf("write saved chain %q: %v", name, err)
			}
		}
	}

	// Arbitrary fixture files, written before any stage runs.
	// Keys are relative paths under tempDir; values are file
	// bodies. A `#!mode:0xxx\n` header on the body is consumed
	// and applied as the file's mode (without becoming part of
	// the written bytes). Defaults to 0600. Parent directories
	// are created with 0700 mode.
	if len(sc.Files) > 0 {
		if err := writeScenarioFiles(tempDir, sc.Files); err != nil {
			t.Fatalf("write scenario files: %v", err)
		}
	}

	// Pin a clean env: HOME → tempdir (no surprise config),
	// XDG_STATE_HOME → per-scenario state dir.
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "config"))
	t.Setenv("XDG_STATE_HOME", stateDir)
	// Clear inherited tokens so the test environment is pristine —
	// otherwise a developer with FORGEJO_TOKEN set inherits it,
	// which leaks into write-path goldens (capturing tokens that
	// aren't part of the scenario). Each forge-touching scenario
	// sets FORGEJO_TOKEN explicitly via stage.env.
	for _, k := range []string{
		"FORGEJO_TOKEN", "FORGEJO_API_URL",
		"GITEA_TOKEN", "GITHUB_TOKEN", "GH_TOKEN",
		"GAIA_PROFILE", "GAIA_PROVIDER",
	} {
		t.Setenv(k, "")
	}

	// chdir to a directory that's NOT a git repo so config
	// auto-detection doesn't pull in the dev's actual project.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	notGit := t.TempDir()
	if err := os.Chdir(notGit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// Optional fake forge — only started when the scenario
	// declares mocks.
	var (
		fakeForge *fakeForge
		apiURL    string
	)
	if len(sc.Mocks) > 0 {
		fakeForge = newFakeForge(t, absScenarioDir, sc.Mocks)
		apiURL = fakeForge.URL()
		t.Cleanup(fakeForge.Close)
	}

	// Stage outputs accumulate so ${token:N} references work.
	stageOuts := make([]string, len(sc.Stages))

	for i, st := range sc.Stages {
		stageOuts[i] = runStage(t, absScenarioDir, i, st, sc, tempDir, stateDir, chainFile, apiURL, fakeForge, stageOuts)
	}
}

// runStage executes one stage and compares against its golden file.
// Returns the captured (un-normalized) stdout so later stages can
// pull a resume_token out of it.
func runStage(
	t *testing.T,
	scenarioDir string,
	index int,
	st stage,
	sc scenario,
	tempDir, stateDir, chainFile, apiURL string,
	forge *fakeForge,
	prior []string,
) string {
	t.Helper()
	stageName := fmt.Sprintf("stage-%d", index)
	t.Run(stageName, func(t *testing.T) {
		// Token substitution.
		subst := func(s string) string {
			s = strings.ReplaceAll(s, "${tempdir}", tempDir)
			s = strings.ReplaceAll(s, "${chainfile}", chainFile)
			s = strings.ReplaceAll(s, "${stateDir}", stateDir)
			s = strings.ReplaceAll(s, "${scenarioDir}", scenarioDir)
			if apiURL != "" {
				s = strings.ReplaceAll(s, "${apiURL}", apiURL)
			}
			// ${token:N} — resume_token from stage N.
			for j := 0; j < index; j++ {
				placeholder := fmt.Sprintf("${token:%d}", j)
				if !strings.Contains(s, placeholder) {
					continue
				}
				tok := mineResumeToken(prior[j])
				if tok == "" {
					t.Fatalf("stage %d references ${token:%d} but stage-%d stdout had no resume_token", index, j, j)
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

		if st.AssertRequest != nil {
			if forge == nil {
				t.Fatal("stage has assert_request but scenario declares no mocks")
			}
			assertRequest(t, forge, st.AssertRequest)
		}

		goldenName := st.Golden
		if goldenName == "" {
			goldenName = stageName + ".golden"
		}
		goldenPath := filepath.Join(scenarioDir, goldenName)

		got := normalize(stdout.String(), tempDir, stateDir, chainFile)
		if apiURL != "" {
			got = strings.ReplaceAll(got, apiURL, "<APIURL>")
		}
		// Scenario-dir paths can leak through `gaia release publish`'s
		// `assets[].path` field (an absolute filesystem path). Normalize
		// the prefix so the golden stays committable.
		got = strings.ReplaceAll(got, scenarioDir, "<SCENARIODIR>")
		if *updateGolden {
			if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
				t.Fatalf("update golden %s: %v", goldenPath, err)
			}
			prior[index] = stdout.String()
			return
		}

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

// mineResumeToken extracts a chain envelope's resume_token from raw
// stdout. Returns "" when the field isn't present.
func mineResumeToken(stdout string) string {
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

// normalize replaces volatile substrings (timing, paths, resume
// tokens, mod_time) with stable placeholders so the golden file is
// committable and the diff stays small over time.
//
// Order matters: replace longest absolute paths first, then the
// shorter regex-based scrubs.
func normalize(s, tempDir, stateDir, chainFile string) string {
	if chainFile != "" {
		s = strings.ReplaceAll(s, chainFile, "<CHAINFILE>")
	}
	if stateDir != "" {
		s = strings.ReplaceAll(s, stateDir, "<STATEDIR>")
	}
	if tempDir != "" {
		s = strings.ReplaceAll(s, tempDir, "<TEMPDIR>")
	}
	s = durationRE.ReplaceAllString(s, `"duration_ms": 0`)
	s = resumeTokenRE.ReplaceAllString(s, `"resume_token": "<TOKEN>"`)
	s = listTokenRE.ReplaceAllString(s, `"token": "<TOKEN>"`)
	s = modTimeRE.ReplaceAllString(s, `"mod_time": "<MODTIME>"`)
	return s
}

var (
	durationRE    = regexp.MustCompile(`"duration_ms":\s*\d+`)
	resumeTokenRE = regexp.MustCompile(`"resume_token":\s*"[0-9a-f]{32}"`)
	// listTokenRE matches the chain-list envelope's per-entry token
	// field. A 32-char hex string is the StateInfo.Token shape;
	// scoping the regex to that prevents it stomping on unrelated
	// "token" keys in other commands' envelopes.
	listTokenRE = regexp.MustCompile(`"token":\s*"[0-9a-f]{32}"`)
	modTimeRE   = regexp.MustCompile(`"mod_time":\s*"[^"]+"`)
)

// fakeForge is the in-process HTTP server that stands in for a real
// Forgejo/Gitea API. The dispatch table is the per-scenario list of
// mockRule entries: the first method+path (and, optionally, query)
// match returns its body. Unmatched requests get 404 with a JSON
// `{"message": "no mock for METHOD PATH"}` so the test failure
// surface points at the missing rule, not at the gaia error.
//
// All requests are recorded for assert_request to inspect.
type fakeForge struct {
	srv      *httptest.Server
	rules    []mockRule
	scenario string

	mu       sync.Mutex
	requests []recordedRequest
}

// recordedRequest captures one inbound request to the fake forge for
// assert_request to read back.
type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   []byte
	Header http.Header
}

func newFakeForge(t *testing.T, scenarioDir string, rules []mockRule) *fakeForge {
	t.Helper()
	f := &fakeForge{rules: rules, scenario: scenarioDir}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *fakeForge) URL() string { return f.srv.URL }
func (f *fakeForge) Close()      { f.srv.Close() }
func (f *fakeForge) Requests() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// LastRequest returns the most recent recorded request, or the zero
// value if none. assert_request uses it to validate write payloads.
func (f *fakeForge) LastRequest() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return recordedRequest{}
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeForge) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()

	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Body:   body,
		Header: r.Header.Clone(),
	})
	f.mu.Unlock()

	for _, rule := range f.rules {
		if !ruleMatches(rule, r) {
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

func ruleMatches(rule mockRule, r *http.Request) bool {
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

func (f *fakeForge) ruleBody(rule mockRule) ([]byte, error) {
	if rule.Body != "" {
		return []byte(rule.Body), nil
	}
	if rule.BodyFile != "" {
		p := filepath.Join(f.scenario, rule.BodyFile)
		return os.ReadFile(p)
	}
	return nil, nil
}

// assertRequest evaluates a stage's AssertRequest against the
// fake-forge's most-recent recorded request.
func assertRequest(t *testing.T, f *fakeForge, a *requestAssertion) {
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

// writeScenarioFiles materializes the scenario.Files map under
// tempDir. Each key is a path relative to tempDir; each value is
// the file body. A leading `#!mode:0xxx\n` header on the body
// (e.g. `#!mode:0644\n`) is parsed and applied as the file mode,
// then stripped from the bytes that get written. Default mode is
// 0600 so credentials fixtures default to safe permissions.
//
// Parent directories are created with mode 0700. The harness
// rejects "/" or ".." segments to keep fixtures contained.
func writeScenarioFiles(tempDir string, files map[string]string) error {
	for rel, body := range files {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("files: %q escapes scenario tempdir", rel)
		}
		mode := os.FileMode(0o600)
		if strings.HasPrefix(body, "#!mode:") {
			nl := strings.IndexByte(body, '\n')
			if nl == -1 {
				return fmt.Errorf("files[%s]: mode header without newline", rel)
			}
			header := body[:nl]
			body = body[nl+1:]
			modeStr := strings.TrimPrefix(header, "#!mode:")
			var parsed uint32
			if _, err := fmt.Sscanf(modeStr, "%o", &parsed); err != nil {
				return fmt.Errorf("files[%s]: parse mode %q: %w", rel, modeStr, err)
			}
			mode = os.FileMode(parsed)
		}
		full := filepath.Join(tempDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return fmt.Errorf("files[%s]: mkdir parent: %w", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), mode); err != nil {
			return fmt.Errorf("files[%s]: write: %w", rel, err)
		}
		// os.WriteFile honours mode only on create; explicit Chmod
		// guarantees the requested permissions even if the file
		// already existed (e.g., from a re-run of the same test).
		if err := os.Chmod(full, mode); err != nil {
			return fmt.Errorf("files[%s]: chmod: %w", rel, err)
		}
	}
	return nil
}
