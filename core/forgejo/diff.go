package forgejo

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// hunkHeaderRe matches `@@ -OldStart[,OldLines] +NewStart[,NewLines] @@ optional-context`.
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// GetPullRequestDiff fetches the raw unified diff from
// /repos/{owner}/{repo}/pulls/{n}.diff and parses it into structured
// DiffFile values. Binary files marshal with Binary=true and no
// Hunks. opts.Paths narrows the result to a subset of file paths
// (matched against either Path or OldPath, so renamed files match by
// either side).
func (p *Provider) GetPullRequestDiff(ctx context.Context, owner, repo string, n int, opts provider.GetPullRequestDiffOptions) ([]types.DiffFile, error) {
	raw, err := p.client.GetRaw(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d.diff", owner, repo, n))
	if err != nil {
		return nil, err
	}
	files, err := ParseUnifiedDiff(string(raw))
	if err != nil {
		return nil, err
	}
	if len(opts.Paths) == 0 {
		return files, nil
	}
	want := make(map[string]bool, len(opts.Paths))
	for _, p := range opts.Paths {
		want[p] = true
	}
	out := files[:0]
	for _, f := range files {
		if want[f.Path] || (f.OldPath != "" && want[f.OldPath]) {
			out = append(out, f)
		}
	}
	return out, nil
}

// ParseUnifiedDiff turns a `git diff`/Forgejo-`.diff` payload into a
// slice of structured DiffFile values. The exported entry-point lives
// in this package (rather than internal/) so a future consumer
// outside the Forgejo provider can reuse it without copying.
//
// Empty input returns no files and no error — that's the correct
// representation of an empty PR (e.g., one that only changes commit
// metadata).
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
	// strings.Split on a string ending with "\n" yields a trailing empty
	// element; drop it so it doesn't get appended as a phantom hunk line.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			oldP, newP, ok := parseDiffGitLine(line)
			if !ok {
				return nil, fmt.Errorf("forgejo: malformed diff header: %q", line)
			}
			current = &types.DiffFile{Path: newP}
			if oldP != newP {
				// We only mark this as renamed when the explicit
				// "rename from / rename to" lines arrive; here just
				// stash the alternate path on OldPath so we can keep
				// it if those lines confirm.
				current.OldPath = oldP
			} else {
				current.OldPath = ""
			}
		case current == nil:
			// Lines outside any file header (preamble, junk) — ignore.
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
			// For a deleted file, the new path side is /dev/null;
			// keep the old path as the visible Path.
			if current.OldPath != "" && current.Path != current.OldPath {
				current.Path = current.OldPath
			}
			current.OldPath = ""
		case strings.HasPrefix(line, "Binary files"):
			current.Binary = true
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			// Path markers — already extracted from `diff --git`.
			// `--- /dev/null` confirms added; `+++ /dev/null` confirms
			// removed; otherwise no-op. Status set above takes
			// precedence.
		case strings.HasPrefix(line, "index "), strings.HasPrefix(line, "similarity index "):
			// Metadata — ignored.
		case strings.HasPrefix(line, "@@"):
			flushHunk()
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			currentHunk = &h
		case currentHunk != nil:
			// Hunk content line. Preserve as-is (with leading +/-/space)
			// so consumers can reconstruct the unified-diff
			// representation.
			currentHunk.Lines = append(currentHunk.Lines, line)
		}
	}
	flushFile()

	// For renamed files: if both rename-from and rename-to arrived, we
	// already have current.Path = new and current.OldPath = old. If
	// the diff just had different `a/X b/Y` paths but no rename
	// directive, we currently still have OldPath stashed; clear it
	// since Status isn't "renamed".
	for i := range files {
		if files[i].Status != "renamed" {
			files[i].OldPath = ""
		}
	}

	return files, nil
}

// parseDiffGitLine extracts (oldPath, newPath) from a
// `diff --git a/X b/Y` line. Paths with spaces are not supported in
// this minimal parser; every test fixture and real Forgejo output we
// have seen avoids them.
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
		return types.Hunk{}, fmt.Errorf("forgejo: malformed hunk header: %q", line)
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
