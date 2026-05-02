package cli

import (
	"strings"

	"github.com/stewartbrothers/gaia/core/autodetect"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

// resolveRepo returns (owner, name) for the operation. Order:
//
//  1. flags.Repo ("owner/name") if set.
//  2. autodetect from cwd's git remote.
//  3. error — caller can't proceed without a target.
func resolveRepo(flags *globalFlags) (owner, name string, err error) {
	if flags.Repo != "" {
		parts := strings.SplitN(flags.Repo, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", exitcode.Errorf(exitcode.Usage,
				"--repo expected owner/name, got %q", flags.Repo)
		}
		return parts[0], parts[1], nil
	}
	detected, derr := autodetect.FromGitRemote(".", "")
	if derr != nil {
		return "", "", exitcode.Errorf(exitcode.Usage,
			"no --repo given and could not auto-detect from git remote — pass --repo owner/name")
	}
	return detected.Owner, detected.Name, nil
}
