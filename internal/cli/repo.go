package cli

import (
	"strings"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/autodetect"
	"github.com/stewartbrothers/gaia/core/config"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

// resolveRepo returns (owner, name) for the operation. Order:
//
//  1. flags.Repo ("owner/name") if set.
//  2. autodetect from cwd's git remote.
//  3. project config's default_repo (.gaia/config.yaml in repo root).
//  4. error — caller can't proceed without a target.
//
// The project-config fallback is the load-bearing one for forges
// where the SSH push host and the HTTPS API host differ (e.g.,
// repo.example.com vs git.example.com): autodetect parses the SSH
// host but the credential store keys by API host, so the autodetect
// result fails to resolve a credential. Pinning default_repo in
// .gaia/config.yaml short-circuits that.
func resolveRepo(flags *globalFlags) (owner, name string, err error) {
	if flags.Repo != "" {
		return splitOwnerName(flags.Repo, "--repo expected owner/name")
	}
	if detected, derr := autodetect.FromGitRemote(".", ""); derr == nil {
		return detected.Owner, detected.Name, nil
	}
	if defaultRepo := projectDefaultRepo(); defaultRepo != "" {
		return splitOwnerName(defaultRepo, "default_repo in .gaia/config.yaml expected owner/name")
	}
	return "", "", exitcode.Errorf(exitcode.Usage,
		"no --repo given, could not auto-detect from git remote, and no default_repo in .gaia/config.yaml — pass --repo owner/name")
}

func splitOwnerName(slug, errPrefix string) (string, string, error) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", exitcode.Errorf(exitcode.Usage,
			"%s, got %q", errPrefix, slug)
	}
	return parts[0], parts[1], nil
}

// projectDefaultRepo reads .gaia/config.yaml from the repo root and
// returns its default_repo, or "" on any failure (no repo, no file,
// parse error). Resolving silently to "" keeps the resolveRepo
// fallback chain readable; the existing global config layer is
// loaded separately by forgebuilder.
func projectDefaultRepo() string {
	root := auth.ProjectRoot(".")
	if root == "" {
		return ""
	}
	cfg, err := config.Load(config.ProjectPath(root))
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.DefaultRepo
}
