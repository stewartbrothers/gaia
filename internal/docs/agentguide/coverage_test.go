package agentguide_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
	"github.com/stewartbrothers/gaia/internal/docs/agentguide"
)

// knownBaselineMissing is the set of top-level commands that the
// audit at PR #246 found absent from docs/agent-guide.md. The
// follow-up bug #271 tracks adding them. Until then this test
// stays green by tolerating exactly this set — if a NEW command
// joins the missing list (e.g. a future PR adds `gaia foo` without
// updating the guide) the diff between this slice and the live
// missing list trips the test.
//
// Removing #271's last entry from this slice and deleting it once
// every command is covered is the unblock path. From then on the
// test is fully active: the missing list must be empty.
var knownBaselineMissing = []string{
	"cache",
	"label",
	"packages",
	"release",
	"server",
	"version",
	"webhook",
	"wiki",
}

// TestAgentGuideMentionsEveryTopLevelCommand is the anti-rot gate
// for #246: every non-meta, non-hidden top-level cobra command on
// `gaia` must be name-dropped (substring `gaia <cmd>`) somewhere
// in docs/agent-guide.md. New commands cannot land without at
// least mentioning themselves in the agent guide.
//
// Substring matching is the bar — quality / depth is human-review
// territory. See coverage_unit_test.go for the unit tests that
// guard the matcher itself.
//
// While #271 is open, the guide carries a known baseline of
// commands that are not yet mentioned. The test fails iff the
// LIVE missing list differs from that baseline — so a regression
// (new command added without a mention) is caught immediately,
// while the existing baseline is tolerated until #271 lands.
func TestAgentGuideMentionsEveryTopLevelCommand(t *testing.T) {
	root := cli.NewRootCmd()
	commands := agentguide.TopLevelCommands(root)
	if len(commands) == 0 {
		t.Fatal("TopLevelCommands returned no commands; cobra wiring broken?")
	}

	guide, err := os.ReadFile(guidePath(t))
	if err != nil {
		t.Fatalf("read agent guide: %v", err)
	}

	missing := agentguide.MissingFromGuide(commands, string(guide))

	// Sort copies so the comparison is order-independent. Both
	// MissingFromGuide and the baseline are already alphabetically
	// ordered by virtue of TopLevelCommands sorting its output, but
	// belt-and-braces in case a future change reorders either.
	gotSorted := append([]string(nil), missing...)
	wantSorted := append([]string(nil), knownBaselineMissing...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)

	if reflect.DeepEqual(gotSorted, wantSorted) {
		// Exactly the known baseline. Skip — the bug issue tracks the
		// fix; once it lands the maintainer empties knownBaselineMissing
		// (and ideally deletes the slice and this skip block) so the
		// test asserts no missing commands at all.
		t.Skipf("agent-guide coverage gap matches known baseline (#271). "+
			"Missing: %v. Remove entries from knownBaselineMissing as the "+
			"guide gains mentions; delete the slice + this skip when empty.",
			missing)
		return
	}

	// Diff the two lists for a precise failure message — added and
	// removed entries reported separately so the maintainer sees
	// at a glance whether a new command slipped in (regression) or
	// the baseline shrank (this slice can be trimmed in lockstep).
	added := diff(gotSorted, wantSorted)
	removed := diff(wantSorted, gotSorted)

	if len(added) > 0 {
		t.Errorf("docs/agent-guide.md is missing mention of new top-level command(s): %v.\n"+
			"Add `gaia <cmd>` somewhere in the guide before merging. If the new "+
			"command genuinely should not be advertised to agents, mark its cobra "+
			"declaration Hidden=true.", added)
	}
	if len(removed) > 0 {
		t.Errorf("knownBaselineMissing in coverage_test.go lists commands now "+
			"covered by the guide: %v. Remove them from the slice (and the slice "+
			"itself, plus the skip block, once empty).", removed)
	}
}

// guidePath resolves docs/agent-guide.md from the test source file
// location. Walks up from the directory of this test until it hits
// the directory containing go.mod, then joins docs/agent-guide.md.
//
// runtime.Caller(0) + go.mod walk-up is the standard pattern for
// "I want a path relative to the repo root that survives `go test`
// being invoked from any cwd".
func guidePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; cannot locate test source")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 16; i++ { // safety cap; gaia tree is shallow
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "docs", "agent-guide.md")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate go.mod walking up from %s", filepath.Dir(thisFile))
	return ""
}

// diff returns elements in a that are not in b. Tiny helper to keep
// the test failure message specific.
func diff(a, b []string) []string {
	bset := make(map[string]struct{}, len(b))
	for _, s := range b {
		bset[s] = struct{}{}
	}
	var out []string
	for _, s := range a {
		if _, ok := bset[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}
