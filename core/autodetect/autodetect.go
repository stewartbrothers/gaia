package autodetect

import (
	"fmt"
	"os/exec"
	"strings"
)

// FromGitRemote runs `git -C dir remote get-url <name>` and parses the
// result. An empty name defaults to "origin". Errors propagate from
// either git (not a repo, no such remote) or the URL parser.
//
// The function shells out rather than reading `.git/config` directly
// so it transparently honors `insteadOf` rewrites and any other git
// config the user has set up.
func FromGitRemote(dir, name string) (*Repo, error) {
	if name == "" {
		name = "origin"
	}
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", name)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("autodetect: git remote get-url %s in %s: %w", name, dir, err)
	}
	return ParseRemoteURL(strings.TrimSpace(string(out)))
}
