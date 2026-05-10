// Package gitignore embeds the recommended .gitignore block for
// projects using gaia and offers a small helper to compare an
// existing .gitignore against the recommended set.
//
// The embedded list is the single source of truth: `gaia gitignore`,
// the corresponding MCP resource, and the docs section in
// docs/configuration.md all read from recommended.txt. Updating the
// list is a one-file edit.
//
// What goes in the recommended list:
//
//   - .gaia/credentials*        — auth.EnsureGitignored already auto-
//     installs this on first `gaia auth ...`, but a project that
//     hand-rolls the credential file or copies it from another
//     checkout can land in-tree without the auto-install path having
//     run. The recommended block makes the entry explicit so a
//     `gaia gitignore --check` in CI catches the gap before the file
//     ever gets committed.
//   - .gaia/insights.db*, .gaia/insights/  — Phase 9 (#238) lands
//     SQLite-backed local insights. Defaults to XDG state, but
//     operators who override the location into the working tree need
//     the SQLite glob siblings (`-wal`, `-shm`) gitignored too. The
//     entries are listed here ahead of Phase 9 shipping so projects
//     can adopt the gitignore once and not need a follow-up edit when
//     insights ships.
//
// .gaia/config.yaml and .gaia/chains/*.yaml are explicitly NOT in
// the list — those are committable project artefacts (non-secret
// defaults, chain definitions). Adding them would push correct
// project state out of version control.
package gitignore

import (
	"bufio"
	_ "embed"
	"strings"
)

// Recommended is the verbatim contents of recommended.txt at build
// time. Consumers should treat the value as read-only; mutating the
// returned string would break every other consumer that shares the
// embed.
//
// The unit test in gitignore_test.go pins this against the
// recommended.txt source so any drift fails CI. The CLI command
// (`gaia gitignore`), the MCP resource (`gaia://gitignore`), and the
// docs section in docs/configuration.md all read from this same
// embed — there is no second copy to keep in sync.
//
//go:embed recommended.txt
var Recommended string

// Entries returns the path-only lines from Recommended, with leading
// and trailing whitespace trimmed. Comment lines (starting with `#`)
// and blank lines are skipped. Used by `gaia gitignore --check` to
// compare against the project's existing .gitignore.
//
// Returned in source order so a caller printing missing entries can
// list them in the same order they appear in recommended.txt — the
// operator's mental model lines up with what they'd append.
func Entries() []string {
	out := make([]string, 0, 8)
	scanner := bufio.NewScanner(strings.NewReader(Recommended))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// Missing returns the recommended entries that do not appear in
// gitignoreContent. The match is line-equality after trimming
// whitespace per line: a `.gitignore` containing `.gaia/credentials*`
// (with optional surrounding blank lines or comments) satisfies the
// `.gaia/credentials*` recommendation.
//
// Returned in recommended.txt source order so output is stable.
//
// A nil/empty gitignoreContent yields the full recommended set as
// missing — exactly what a project with no .gitignore at all needs to
// see when it runs `gaia gitignore --check`.
func Missing(gitignoreContent string) []string {
	present := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(gitignoreContent))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		present[line] = true
	}
	var missing []string
	for _, want := range Entries() {
		if !present[want] {
			missing = append(missing, want)
		}
	}
	return missing
}
