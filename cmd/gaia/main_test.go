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
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

	// Stages run sequentially. Each one produces a golden file
	// named "stage-N.golden" by default (override per-stage with
	// stage.golden).
	Stages []stage `yaml:"stages"`
}

// stage is one gaia invocation within a scenario.
//
// Argv tokens supported:
//
//	${tempdir}    — scenario tempdir (HOME also points here).
//	${chainfile}  — path to the written ChainYAML.
//	${stateDir}   — XDG_STATE_HOME for the scenario.
//	${token:N}    — resume_token mined from stage N's stdout
//	                envelope. Lets a "resume" stage reference
//	                what a prior "run" stage yielded.
type stage struct {
	Args     []string          `yaml:"args"`
	Env      map[string]string `yaml:"env,omitempty"`
	ExitCode int               `yaml:"exit_code,omitempty"`
	Golden   string            `yaml:"golden,omitempty"`

	// Description: docs-only.
	Description string `yaml:"description,omitempty"`
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

	// Pin a clean env: HOME → tempdir (no surprise config),
	// XDG_STATE_HOME → per-scenario state dir.
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "config"))
	t.Setenv("XDG_STATE_HOME", stateDir)

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

	// Stage outputs accumulate so ${token:N} references work.
	stageOuts := make([]string, len(sc.Stages))

	for i, st := range sc.Stages {
		stageOuts[i] = runStage(t, absScenarioDir, i, st, sc, tempDir, stateDir, chainFile, stageOuts)
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
	tempDir, stateDir, chainFile string,
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

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		execErr := root.ExecuteContext(ctx)
		gotExit := exitcode.Of(execErr)
		if gotExit != st.ExitCode {
			t.Errorf("exit code: got %d want %d\nstderr: %s\nstdout: %s",
				gotExit, st.ExitCode, stderr.String(), stdout.String())
		}

		goldenName := st.Golden
		if goldenName == "" {
			goldenName = stageName + ".golden"
		}
		goldenPath := filepath.Join(scenarioDir, goldenName)

		got := normalize(stdout.String(), tempDir, stateDir, chainFile)
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
