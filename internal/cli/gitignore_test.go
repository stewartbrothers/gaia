package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
	"github.com/stewartbrothers/gaia/internal/gitignore"
)

// TestGitignoreDefaultPrintsEmbedded — the bare command emits the
// embedded recommended block verbatim, suitable for redirection
// into a project .gitignore.
func TestGitignoreDefaultPrintsEmbedded(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"gitignore"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if got := stdout.String(); got != gitignore.Recommended {
		t.Fatalf("`gaia gitignore` output drifts from gitignore.Recommended\n"+
			"len(got)=%d len(want)=%d", len(got), len(gitignore.Recommended))
	}
}

// TestGitignoreFormatJSONReturnsEnvelope — `--format json` opts into
// the standard envelope shape so MCP-style consumers can branch on
// schema_version like every other gaia command.
func TestGitignoreFormatJSONReturnsEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "json", "gitignore"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	var env struct {
		SchemaVersion string `json:"schema_version"`
		Data          struct {
			Entries []string `json:"entries"`
			Missing []string `json:"missing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\nstdout: %s", err, stdout.String())
	}
	if env.SchemaVersion == "" {
		t.Errorf("schema_version empty")
	}
	if len(env.Data.Entries) == 0 {
		t.Errorf("data.entries empty; want recommended block entries")
	}
	if len(env.Data.Missing) != 0 {
		t.Errorf("data.missing populated on print path: %v", env.Data.Missing)
	}
}

// TestGitignoreCheckHappy — when .gitignore covers every recommended
// entry, --check exits 0 and emits a single confirmation line.
func TestGitignoreCheckHappy(t *testing.T) {
	dir := t.TempDir()
	full := strings.Join(gitignore.Entries(), "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"gitignore", "--check"})

	err := root.Execute()
	if got := exitcode.Of(err); got != exitcode.OK {
		t.Errorf("exit code: got %d want %d\nstderr: %s\nstdout: %s",
			got, exitcode.OK, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "covers every recommended entry") {
		t.Errorf("expected confirmation message; got: %s", stdout.String())
	}
}

// TestGitignoreCheckMissing — when entries are absent, --check exits
// non-zero and lists every missing entry.
func TestGitignoreCheckMissing(t *testing.T) {
	dir := t.TempDir()
	// Only one entry present; rest are missing.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".gaia/credentials*\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"gitignore", "--check"})

	err := root.Execute()
	if got := exitcode.Of(err); got == exitcode.OK {
		t.Errorf("exit code: got OK want non-zero\nstderr: %s\nstdout: %s",
			stderr.String(), stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "missing") {
		t.Errorf("expected 'missing' in output; got: %s", out)
	}
	for _, want := range []string{
		".gaia/insights.db",
		".gaia/insights.db-wal",
		".gaia/insights.db-shm",
		".gaia/insights/",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing entry %q\nfull output: %s", want, out)
		}
	}
	if strings.Contains(out, ".gaia/credentials*\n") {
		t.Errorf("output should not flag .gaia/credentials*; it is present in fixture\nfull output: %s", out)
	}
}

// TestGitignoreCheckQuietMissing — --check --quiet produces no
// output but exits non-zero so CI scripts can branch on $?.
func TestGitignoreCheckQuietMissing(t *testing.T) {
	dir := t.TempDir()
	// No .gitignore at all → all entries missing.

	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"gitignore", "--check", "--quiet"})

	err := root.Execute()
	if got := exitcode.Of(err); got == exitcode.OK {
		t.Errorf("exit code: got OK want non-zero")
	}
	if got := stdout.String(); got != "" {
		t.Errorf("--quiet should produce no stdout; got: %s", got)
	}
}

// TestGitignoreCheckQuietHappy — --check --quiet on a fully-covered
// .gitignore is silent and exits 0.
func TestGitignoreCheckQuietHappy(t *testing.T) {
	dir := t.TempDir()
	full := strings.Join(gitignore.Entries(), "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"gitignore", "--check", "--quiet"})

	if err := root.Execute(); err != nil {
		t.Errorf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if got := stdout.String(); got != "" {
		t.Errorf("--quiet should produce no stdout on happy path; got: %s", got)
	}
}

// TestGitignoreCheckPathFlag — --path lets a CI script point at a
// .gitignore in a different location (a monorepo subdirectory, for
// example) without chdir.
func TestGitignoreCheckPathFlag(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "subdir", ".gitignore")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	full := strings.Join(gitignore.Entries(), "\n") + "\n"
	if err := os.WriteFile(target, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"gitignore", "--check", "--path", target})

	if err := root.Execute(); err != nil {
		t.Errorf("Execute: %v\nstderr: %s", err, stderr.String())
	}
}

// TestGitignorePrettyHappy — `--format pretty` on the print path
// emits the embedded block verbatim. Same behaviour as the default
// raw-text path; the pretty renderer exists for command-shape parity
// with every other gaia subcommand, not because the block needs
// further rendering.
func TestGitignorePrettyHappy(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "pretty", "gitignore"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if got := stdout.String(); got != gitignore.Recommended {
		t.Errorf("--format pretty drift; len(got)=%d len(want)=%d", len(got), len(gitignore.Recommended))
	}
}

// TestGitignorePrettyCheckMissing — `--format pretty --check`
// renders the missing-entries banner + per-entry list, matching the
// raw-text path. Pretty output stays human-readable when --check
// fails.
func TestGitignorePrettyCheckMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".gaia/credentials*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "pretty", "gitignore", "--check"})

	err := root.Execute()
	if got := exitcode.Of(err); got == exitcode.OK {
		t.Errorf("exit code: got OK want non-zero")
	}
	out := stdout.String()
	if !strings.Contains(out, "missing") {
		t.Errorf("expected 'missing' in pretty output; got: %s", out)
	}
	if !strings.Contains(out, ".gaia/insights.db") {
		t.Errorf("expected .gaia/insights.db in pretty output; got: %s", out)
	}
}

// TestGitignoreCheckJSON — --check --format json emits the standard
// envelope with both entries and missing populated, and still exits
// non-zero when entries are missing.
func TestGitignoreCheckJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".gaia/credentials*\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "json", "gitignore", "--check"})

	err := root.Execute()
	if got := exitcode.Of(err); got == exitcode.OK {
		t.Errorf("exit code: got OK want non-zero")
	}

	var env struct {
		Data struct {
			Entries []string `json:"entries"`
			Missing []string `json:"missing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data.Missing) == 0 {
		t.Errorf("data.missing empty; expected entries to be flagged")
	}
}
