package gitignore_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/internal/gitignore"
)

// TestEmbeddedMatchesSource is the anti-drift gate: the bytes
// `gaia gitignore` serves must equal recommended.txt on disk. If a
// developer edits the embed declaration without updating the file
// (or vice versa) this test fires before the divergence ships.
func TestEmbeddedMatchesSource(t *testing.T) {
	want, err := os.ReadFile("recommended.txt")
	if err != nil {
		t.Fatalf("read recommended.txt: %v", err)
	}
	if got := gitignore.Recommended; got != string(want) {
		t.Fatalf("embedded gitignore drifts from recommended.txt\n"+
			"len(embed)=%d len(file)=%d", len(got), len(want))
	}
}

// TestRecommendedHasContent guards against the embed accidentally
// becoming empty (e.g. file moved without updating the directive).
func TestRecommendedHasContent(t *testing.T) {
	if gitignore.Recommended == "" {
		t.Fatal("gitignore.Recommended is empty; //go:embed directive likely broken")
	}
	if !strings.Contains(gitignore.Recommended, ".gaia/credentials*") {
		t.Errorf("recommended block missing .gaia/credentials* entry")
	}
}

// TestEntriesSkipsCommentsAndBlanks — the helper returns the path
// lines only. Comments and blank lines (the structure that makes
// recommended.txt readable) must not leak into the comparison set.
func TestEntries(t *testing.T) {
	entries := gitignore.Entries()
	if len(entries) == 0 {
		t.Fatal("Entries() returned empty slice; expected at least .gaia/credentials*")
	}
	for _, e := range entries {
		if strings.HasPrefix(e, "#") {
			t.Errorf("Entries() leaked comment line: %q", e)
		}
		if strings.TrimSpace(e) == "" {
			t.Errorf("Entries() leaked blank line")
		}
	}
	// Pin the canonical members so a future edit of recommended.txt
	// that drops one of them is caught here, not in a downstream
	// consumer.
	must := []string{
		".gaia/credentials*",
		".gaia/insights.db",
		".gaia/insights.db-wal",
		".gaia/insights.db-shm",
		".gaia/insights/",
	}
	for _, m := range must {
		var found bool
		for _, e := range entries {
			if e == m {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Entries() missing required entry %q; got %v", m, entries)
		}
	}
}

// TestMissingEmptyContentReturnsAllEntries — a fresh repo with no
// .gitignore should be told every recommended entry is missing.
func TestMissingEmptyContentReturnsAllEntries(t *testing.T) {
	missing := gitignore.Missing("")
	want := gitignore.Entries()
	if len(missing) != len(want) {
		t.Errorf("Missing(\"\") returned %d entries; want %d", len(missing), len(want))
	}
	for i := range missing {
		if missing[i] != want[i] {
			t.Errorf("Missing[%d]: got %q want %q (order should match Entries)",
				i, missing[i], want[i])
		}
	}
}

// TestMissingFullySatisfied — every recommended entry present in
// the input means an empty missing slice.
func TestMissingFullySatisfied(t *testing.T) {
	full := strings.Join(gitignore.Entries(), "\n") + "\n"
	missing := gitignore.Missing(full)
	if len(missing) != 0 {
		t.Errorf("Missing(<all entries>) = %v; want empty", missing)
	}
}

// TestMissingPartialOverlap — only the entries not present in
// content are reported. Order matches recommended.txt.
func TestMissingPartialOverlap(t *testing.T) {
	// .gitignore-style content: a project that already has
	// .gaia/credentials* but hasn't added the insights-DB entries.
	content := "# legacy entry\n*.log\n.gaia/credentials*\n"
	missing := gitignore.Missing(content)
	if len(missing) == 0 {
		t.Fatal("Missing() = empty; expected the insights-DB entries to be missing")
	}
	for _, e := range missing {
		if e == ".gaia/credentials*" {
			t.Errorf("Missing() reported %q as missing; it is present in content", e)
		}
	}
	// Insights entries should be missing.
	wantMissing := map[string]bool{
		".gaia/insights.db":     true,
		".gaia/insights.db-wal": true,
		".gaia/insights.db-shm": true,
		".gaia/insights/":       true,
	}
	for _, e := range missing {
		if !wantMissing[e] {
			t.Errorf("Missing() returned unexpected entry %q", e)
		}
	}
}

// TestMissingIgnoresGitignoreCommentsAndBlanks — a comment in the
// project's .gitignore that matches a recommended path verbatim does
// not satisfy the recommendation. Comments and blank lines on either
// side are stripped before comparison.
func TestMissingIgnoresGitignoreCommentsAndBlanks(t *testing.T) {
	// A lookalike comment ("# .gaia/credentials*") must NOT count as
	// the path being present.
	content := "# .gaia/credentials*\n\n# unrelated\n"
	missing := gitignore.Missing(content)
	var found bool
	for _, e := range missing {
		if e == ".gaia/credentials*" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Missing() did not flag .gaia/credentials* despite only a lookalike comment in content")
	}
}
