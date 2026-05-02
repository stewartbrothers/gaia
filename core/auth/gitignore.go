package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// EnsureGitignored adds entry to repoRoot/.gitignore if it isn't
// already present. Idempotent: scans existing entries; appends only
// if missing. Creates the file if it doesn't exist. Preserves the
// trailing-newline convention so a manually-edited .gitignore isn't
// disturbed.
func EnsureGitignored(repoRoot, entry string) error {
	path := filepath.Join(repoRoot, ".gitignore")
	body, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("auth: read %s: %w", path, err)
	}

	if hasGitignoreEntry(string(body), entry) {
		return nil
	}

	prefix := string(body)
	if len(prefix) > 0 && !strings.HasSuffix(prefix, "\n") {
		prefix += "\n"
	}
	prefix += entry + "\n"

	if err := os.WriteFile(path, []byte(prefix), 0o644); err != nil {
		return fmt.Errorf("auth: write %s: %w", path, err)
	}
	return nil
}

// hasGitignoreEntry reports whether body contains entry on its own
// line (possibly with surrounding whitespace). Comments (`#`-prefixed)
// lines are ignored.
func hasGitignoreEntry(body, entry string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == entry {
			return true
		}
	}
	return false
}
