package agentguide_test

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/internal/docs/agentguide"
)

// fixtureRoot constructs a small cobra tree mirroring the shape of
// the real gaia root: a handful of regular subcommands, a hidden
// command (must be skipped), and a meta-command (`completion`,
// auto-added by cobra in the real tree but added explicitly here so
// the test has something concrete to assert against).
func fixtureRoot() *cobra.Command {
	root := &cobra.Command{Use: "gaia"}
	root.AddCommand(&cobra.Command{Use: "issue"})
	root.AddCommand(&cobra.Command{Use: "pr"})
	root.AddCommand(&cobra.Command{Use: "completion"})
	root.AddCommand(&cobra.Command{Use: "help"})
	root.AddCommand(&cobra.Command{Use: "secret-debug", Hidden: true})
	// Add in non-alphabetical order so we exercise the sort.
	root.AddCommand(&cobra.Command{Use: "auth"})
	return root
}

// TestTopLevelCommands_FiltersHiddenAndMeta is the core invariant:
// hidden commands and the meta allowlist (help, completion) drop
// out; everything else is enumerated, sorted.
func TestTopLevelCommands_FiltersHiddenAndMeta(t *testing.T) {
	got := agentguide.TopLevelCommands(fixtureRoot())
	want := []string{"auth", "issue", "pr"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TopLevelCommands fixture mismatch\n got: %v\nwant: %v", got, want)
	}
}

// TestTopLevelCommands_NilRoot — a nil root must not panic. Defensive
// against a future caller wiring without checking.
func TestTopLevelCommands_NilRoot(t *testing.T) {
	if got := agentguide.TopLevelCommands(nil); got != nil {
		t.Fatalf("nil root: want nil, got %v", got)
	}
}

// TestMissingFromGuide_AllPresent is the pass path. Every command
// is mentioned somewhere in the guide; the missing slice is empty.
func TestMissingFromGuide_AllPresent(t *testing.T) {
	guide := `# Fixture
This guide mentions gaia issue, gaia pr review, and gaia auth forgejo
in prose. Subcommand mentions cover the parent (substring match).`
	missing := agentguide.MissingFromGuide([]string{"issue", "pr", "auth"}, guide)
	if len(missing) != 0 {
		t.Fatalf("expected no missing commands, got %v", missing)
	}
}

// TestMissingFromGuide_SomeMissing is the fail path. The reported
// list must contain exactly the commands whose `gaia <cmd>` token
// is absent — order preserved from the input slice.
func TestMissingFromGuide_SomeMissing(t *testing.T) {
	guide := `Mentions gaia issue but never names the others.`
	missing := agentguide.MissingFromGuide(
		[]string{"issue", "pr", "wiki"},
		guide,
	)
	want := []string{"pr", "wiki"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing mismatch\n got: %v\nwant: %v", missing, want)
	}
}

// TestMissingFromGuide_RequiresGaiaPrefix — a bare command name
// without the `gaia ` prefix does not count as coverage. Otherwise
// any prose mention of `issue` would falsely satisfy the rule.
func TestMissingFromGuide_RequiresGaiaPrefix(t *testing.T) {
	guide := `An issue tracker is great. The pr workflow is fine. Wiki rocks.`
	missing := agentguide.MissingFromGuide(
		[]string{"issue", "pr", "wiki"},
		guide,
	)
	want := []string{"issue", "pr", "wiki"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing mismatch\n got: %v\nwant: %v", missing, want)
	}
}

// TestMissingFromGuide_EmptyInputs — defensive: empty command list
// returns nil regardless of guide content; empty guide reports
// every command as missing.
func TestMissingFromGuide_EmptyInputs(t *testing.T) {
	if got := agentguide.MissingFromGuide(nil, "anything"); got != nil {
		t.Fatalf("empty commands: want nil, got %v", got)
	}
	got := agentguide.MissingFromGuide([]string{"issue"}, "")
	want := []string{"issue"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty guide mismatch\n got: %v\nwant: %v", got, want)
	}
}
