package agentguide_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
	"github.com/stewartbrothers/gaia/internal/docs/agentguide"
)

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
// #271 closed the original audit gap by adding mentions for every
// then-missing command, so this test is now fully active: the
// missing list must be empty on every run.
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

	if len(missing) > 0 {
		t.Errorf("docs/agent-guide.md is missing mention of top-level command(s): %v.\n"+
			"Add `gaia <cmd>` somewhere in the guide before merging. If the new "+
			"command genuinely should not be advertised to agents, mark its cobra "+
			"declaration Hidden=true.", missing)
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
