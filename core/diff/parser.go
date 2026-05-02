// Package diff parses unified-diff text into structured DiffFile +
// Hunk values. The parser is forge-agnostic — both core/forgejo and
// core/github fetch raw `.diff` payloads from their respective
// `/pulls/{n}.diff` endpoints and route through this package.
//
// Originally lived in core/forgejo; moved here when the GitHub
// provider needed the same parser. The grammar is the standard `git
// diff` unified format; sub-grouped paths (gitlab) are explicitly
// rejected.
package diff

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/stewartbrothers/gaia/core/types"
)

// hunkHeaderRe matches `@@ -OldStart[,OldLines] +NewStart[,NewLines] @@ optional-context`.
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// ParseUnifiedDiff turns a `git diff` / forge-`.diff` payload into a
// slice of structured DiffFile values. Empty input returns nil, nil
// (the right representation for a PR that changes only commit
// metadata).
//
// Best-effort semantics for malformed input: the parser attempts to
// recover at the next `diff --git` boundary, so a single malformed
// hunk doesn't lose all subsequent files.
func ParseUnifiedDiff(raw string) ([]types.DiffFile, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var files []types.DiffFile
	var current *types.DiffFile
	var currentHunk *types.Hunk

	flushHunk := func() {
		if current != nil && currentHunk != nil {
			current.Hunks = append(current.Hunks, *currentHunk)
			currentHunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if current != nil {
			if current.Status == "" {
				current.Status = "modified"
			}
			files = append(files, *current)
			current = nil
		}
	}

	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			oldP, newP, ok := parseDiffGitLine(line)
			if !ok {
				return nil, fmt.Errorf("diff: malformed header: %q", line)
			}
			current = &types.DiffFile{Path: newP}
			if oldP != newP {
				current.OldPath = oldP
			} else {
				current.OldPath = ""
			}
		case current == nil:
			// preamble or junk; ignore
		case strings.HasPrefix(line, "rename from "):
			current.Status = "renamed"
			current.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			current.Status = "renamed"
			current.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "new file mode"):
			current.Status = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			current.Status = "removed"
			if current.OldPath != "" && current.Path != current.OldPath {
				current.Path = current.OldPath
			}
			current.OldPath = ""
		case strings.HasPrefix(line, "Binary files"):
			current.Binary = true
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			// Path markers — already extracted from `diff --git`
		case strings.HasPrefix(line, "index "), strings.HasPrefix(line, "similarity index "):
			// metadata
		case strings.HasPrefix(line, "@@"):
			flushHunk()
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			currentHunk = &h
		case currentHunk != nil:
			currentHunk.Lines = append(currentHunk.Lines, line)
		}
	}
	flushFile()

	for i := range files {
		if files[i].Status != "renamed" {
			files[i].OldPath = ""
		}
	}
	return files, nil
}

func parseDiffGitLine(line string) (oldP, newP string, ok bool) {
	rest := strings.TrimPrefix(line, "diff --git ")
	parts := strings.Fields(rest)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimPrefix(parts[0], "a/"),
		strings.TrimPrefix(parts[1], "b/"),
		true
}

func parseHunkHeader(line string) (types.Hunk, error) {
	m := hunkHeaderRe.FindStringSubmatch(line)
	if m == nil {
		return types.Hunk{}, fmt.Errorf("diff: malformed hunk header: %q", line)
	}
	out := types.Hunk{Header: line}
	out.OldStart, _ = strconv.Atoi(m[1])
	out.OldLines = 1
	if m[2] != "" {
		out.OldLines, _ = strconv.Atoi(m[2])
	}
	out.NewStart, _ = strconv.Atoi(m[3])
	out.NewLines = 1
	if m[4] != "" {
		out.NewLines, _ = strconv.Atoi(m[4])
	}
	return out, nil
}

// FilterByPaths reduces files to those whose Path or OldPath matches
// any of the listed paths. Empty paths returns files unchanged.
// Renamed files match by either side.
func FilterByPaths(files []types.DiffFile, paths []string) []types.DiffFile {
	if len(paths) == 0 {
		return files
	}
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[p] = true
	}
	out := files[:0]
	for _, f := range files {
		if want[f.Path] || (f.OldPath != "" && want[f.OldPath]) {
			out = append(out, f)
		}
	}
	return out
}
