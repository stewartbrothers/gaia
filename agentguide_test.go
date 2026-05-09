package agentguide_test

import (
	"os"
	"strings"
	"testing"

	agentguide "github.com/stewartbrothers/gaia"
)

// TestEmbeddedMatchesSource is the anti-drift gate: the bytes
// `gaia learn` serves must be identical to docs/agent-guide.md on
// disk. If a developer edits the docs without rebuilding, or edits
// the embed declaration without updating the docs, this test
// catches it before the divergence ships.
func TestEmbeddedMatchesSource(t *testing.T) {
	want, err := os.ReadFile("docs/agent-guide.md")
	if err != nil {
		t.Fatalf("read docs/agent-guide.md: %v", err)
	}
	if got := agentguide.Markdown; got != string(want) {
		t.Fatalf("embedded agent-guide drifts from docs/agent-guide.md\n"+
			"len(embed)=%d len(file)=%d", len(got), len(want))
	}
}

// TestEmbeddedHasContent guards against the embed accidentally
// becoming empty (e.g. file moved without updating the directive).
// Cheap belt-and-braces — the source-equality test above already
// catches a zero-length file, but this gives a clearer signal.
func TestEmbeddedHasContent(t *testing.T) {
	if agentguide.Markdown == "" {
		t.Fatal("agentguide.Markdown is empty; //go:embed directive likely broken")
	}
	if !strings.HasPrefix(agentguide.Markdown, "# Agent guide") {
		t.Fatalf("expected embed to start with '# Agent guide' heading; got first 40 chars: %q",
			agentguide.Markdown[:min(40, len(agentguide.Markdown))])
	}
}
