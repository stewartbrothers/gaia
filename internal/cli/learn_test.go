package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	agentguide "github.com/stewartbrothers/gaia"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// TestLearnDefaultMarkdown asserts the bare `gaia learn` command
// emits the embedded markdown verbatim — no JSON envelope, no
// trailing newline mangling. This is the inverted-default the issue
// (#243) calls for: the agent reads the doc directly, not a wrapped
// payload.
func TestLearnDefaultMarkdown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"learn"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	got := stdout.String()
	if got != agentguide.Markdown {
		t.Fatalf("`gaia learn` output drifts from agentguide.Markdown\n"+
			"len(got)=%d len(want)=%d", len(got), len(agentguide.Markdown))
	}
	if !strings.HasPrefix(got, "# Agent guide") {
		t.Errorf("expected output to start with '# Agent guide'; got first 40 chars: %q",
			got[:min(40, len(got))])
	}
}

// TestLearnFormatPrettyMarkdown — `--format pretty` is also raw
// markdown (the guide is already structured prose; wrapping it in a
// pretty renderer would be noise).
func TestLearnFormatPrettyMarkdown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "pretty", "learn"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	got := stdout.String()
	if got != agentguide.Markdown {
		t.Fatalf("`gaia --format pretty learn` should emit raw markdown identical to agentguide.Markdown\n"+
			"len(got)=%d len(want)=%d", len(got), len(agentguide.Markdown))
	}
}

// TestLearnFormatJSON asserts the envelope shape. Length matches
// the embed; content matches the embed; version matches whatever
// internal/version exposes (could be "dev" in test runs).
func TestLearnFormatJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "json", "learn"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	var env struct {
		SchemaVersion string `json:"schema_version"`
		Data          struct {
			Content string `json:"content"`
			Length  int    `json:"length"`
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\nstdout: %s", err, stdout.String())
	}
	if env.SchemaVersion == "" {
		t.Errorf("schema_version empty")
	}
	if env.Data.Content != agentguide.Markdown {
		t.Errorf("data.content drifts from agentguide.Markdown\n"+
			"len(content)=%d len(embed)=%d", len(env.Data.Content), len(agentguide.Markdown))
	}
	if env.Data.Length != len(agentguide.Markdown) {
		t.Errorf("data.length=%d want %d", env.Data.Length, len(agentguide.Markdown))
	}
	if env.Data.Version == "" {
		t.Errorf("data.version empty")
	}
}

// TestRootLongContainsAgentCallout — the bare `gaia` and
// `gaia --help` outputs must surface the agent callout so an AI
// agent that runs the binary cold sees the pointer to `gaia learn`.
// Asserts the substring is present in the root command's Long
// description (which cobra renders for both `gaia` with no args and
// `gaia --help`).
func TestRootLongContainsAgentCallout(t *testing.T) {
	root := cli.NewRootCmd()
	if !strings.Contains(root.Long, "gaia learn") {
		t.Errorf("root.Long does not mention `gaia learn`; agent callout missing\nroot.Long:\n%s", root.Long)
	}
	if !strings.Contains(root.Long, "AI coding agent") {
		t.Errorf("root.Long missing `AI coding agent` callout phrasing\nroot.Long:\n%s", root.Long)
	}
}

// TestRootHelpOutputContainsAgentCallout drives the actual --help
// rendering through cobra to confirm the callout reaches stdout.
// Belt-and-braces: catches a regression where someone accidentally
// removes the callout from .Long but leaves a different mention
// elsewhere.
func TestRootHelpOutputContainsAgentCallout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "gaia learn") {
		t.Errorf("`gaia --help` does not mention `gaia learn`\n--- stdout ---\n%s", out)
	}
}
