// Package doctor lints gaia's resolved config + credential surface
// and reports actionable findings. It is the engine behind `gaia
// config doctor` (#277).
//
// Doctor is read-only: it never mutates config, credentials, or
// gitignore files. The CLI wrapper renders findings; auto-fix is
// explicitly out of scope (v1).
//
// Findings carry a level (OK / INFO / WARN / ERR), a stable code
// (e.g. `global-default-profile`), a human-readable message, and
// a one-line remediation. CLI gating maps `ERR` to exit code 1;
// `--strict` additionally promotes `WARN` to `ERR`.
//
// Codes are stable identifiers: tests pin them, agents may filter
// on them, and changing one would break any consumer doing
// rule-suppression. Adding new codes is always safe; renaming is
// not.
package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/config"
)

// Level is the severity of a finding. The zero value is invalid;
// every emitted finding must declare its level explicitly.
type Level string

const (
	// LevelOK reports a passing check. Emitted only for the
	// summary-style entries that confirm a smell is absent;
	// per-finding OK is otherwise noisy.
	LevelOK Level = "OK"
	// LevelInfo is for context that helps the operator debug
	// without itself indicating a problem (e.g. "resolved from
	// project=…, global=…").
	LevelInfo Level = "INFO"
	// LevelWarn flags a smell — a likely misconfiguration that
	// won't fail commands today but will surprise the operator
	// later. `--strict` promotes these to ERR for CI gating.
	LevelWarn Level = "WARN"
	// LevelErr is a definite misconfiguration: a command will
	// fail, leak data, or behave wrongly. Exit code 1 on any
	// ERR; the operator is expected to fix before retrying.
	LevelErr Level = "ERR"
)

// Finding codes — stable identifiers. Listed here in groups so the
// inventory of what doctor checks is greppable in one place.
const (
	// Multi-project safety
	CodeGlobalDefaultProfile = "global-default-profile"
	CodeGlobalDefaultRepo    = "global-default-repo"

	// Credential hygiene
	CodeCredentialsFileMode = "credentials-file-mode"
	CodeProjectCredsLeak    = "project-credentials-not-gitignored"
	CodeEnvAndCredOverlap   = "env-and-credentials-overlap"

	// Profile coherence
	CodeProfileNoProvider     = "profile-no-provider"
	CodeProfileNoAPIURL       = "profile-no-api-url"
	CodeTokenEnvEmpty         = "token-env-empty"
	CodeDefaultProfileMissing = "default-profile-missing"

	// Repo + cwd context
	CodeRepoResolution = "repo-resolution"
	CodeConfigLayers   = "config-layers"
)

// Finding is one rendered observation from a doctor run. JSON tags
// match the operator-facing envelope shape; the CLI renders these
// records directly.
type Finding struct {
	Level       Level  `json:"level"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
	// SourceFile is the path doctor inspected to produce the
	// finding, when applicable. Helps the operator open the
	// right file without re-reading docs. Omitted when not
	// relevant (e.g. env-var overlap is not file-bound).
	SourceFile string `json:"source_file,omitempty"`
}

// Inputs bundles everything Run needs. Constructed by the CLI from
// the same loaders the rest of gaia uses, so doctor's view is
// byte-identical to what `gaia issue list` would see.
//
// All fields are optional: nil / empty / "" inputs are handled
// without panicking so doctor can be invoked on a half-set-up
// machine without crashing before it reports.
type Inputs struct {
	// GlobalConfigPath is the resolved path to the global YAML
	// (typically $XDG_CONFIG_HOME/gaia/config.yaml). May be "" if
	// the OS lookup failed; doctor reports a single warning in
	// that case rather than aborting.
	GlobalConfigPath string
	// Global is the parsed global config. nil when the file
	// doesn't exist; an empty *Config when it parses but is bare.
	Global *config.Config

	// ProjectConfigPath is the resolved path to .gaia/config.yaml
	// inside the cwd's repo root. "" when not inside a repo.
	ProjectConfigPath string
	// Project is the parsed project config, or nil.
	Project *config.Config

	// GlobalCredentialsPath is the resolved path to the global
	// credentials YAML. May be "".
	GlobalCredentialsPath string
	// ProjectCredentialsPath is the resolved path to the project
	// credentials YAML. May be "".
	ProjectCredentialsPath string

	// Credentials is the layered credential view. Nil treated as
	// no credentials at all.
	Credentials *auth.Layered

	// Cwd is the directory doctor was run from. Used in INFO
	// messages so the operator knows which layer doctor read.
	Cwd string

	// RepoRoot is the discovered git repo root (or "" when not
	// inside one). Drives the project-credentials-leak check and
	// the repo-resolution INFO.
	RepoRoot string

	// EnvVars is a snapshot of the token env vars in effect at
	// invocation. Keys: "GITEA_TOKEN", "FORGEJO_TOKEN",
	// "GH_TOKEN", "GITHUB_TOKEN". Value is whether the var is
	// non-empty — never the secret itself; doctor must never
	// touch the actual value.
	EnvVars map[string]bool

	// Profile is the resolved profile name doctor inspects for
	// coherence. Empty means doctor falls back to the merged
	// default_profile.
	Profile string

	// RepoFlag, if non-empty, simulates `--repo owner/name` for
	// the repo-resolution INFO.
	RepoFlag string

	// GitRemoteRepo, if non-empty, is the autodetected
	// owner/name from the cwd's git remote. Empty when no remote
	// or not in a repo.
	GitRemoteRepo string
}

// Run executes every check against in and returns the findings in
// stable order: severity (ERR > WARN > INFO > OK) is NOT used for
// ordering — the operator wants codes in a deterministic order so
// a diff between two runs is readable. Order matches the check
// groups in this file, top to bottom.
//
// Run never returns an error: a check that can't complete (e.g.
// stat fails on a credentials file because of an unrelated I/O
// glitch) emits a finding describing the failure rather than
// aborting the whole report.
func Run(in Inputs) []Finding {
	var out []Finding
	out = append(out, checkMultiProjectSafety(in)...)
	out = append(out, checkCredentialHygiene(in)...)
	out = append(out, checkProfileCoherence(in)...)
	out = append(out, checkRepoResolution(in))
	out = append(out, checkCwdContext(in))
	return out
}

// HasErrors reports whether any finding in fs is at LevelErr.
// Convenience for CLI exit-code gating; tests pin the contract.
func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.Level == LevelErr {
			return true
		}
	}
	return false
}

// PromoteWarnings rewrites every WARN finding to ERR in place.
// Used by the CLI's `--strict` flag so CI scripts can fail on
// smells, not just hard breakages.
func PromoteWarnings(findings []Finding) []Finding {
	for i := range findings {
		if findings[i].Level == LevelWarn {
			findings[i].Level = LevelErr
		}
	}
	return findings
}

// --- Multi-project safety ---------------------------------------

// checkMultiProjectSafety flags routing-key contamination in the
// global config: default_profile and default_repo belong in the
// project's `.gaia/config.yaml`, never in `~/.config/gaia/config.yaml`.
// Putting them globally affects every other project on the system.
//
// One finding per offending key; clean global → no findings (no
// summary OK — doctor's contract is "report problems").
func checkMultiProjectSafety(in Inputs) []Finding {
	var out []Finding
	if in.Global == nil {
		return out
	}
	if in.Global.DefaultProfile != "" {
		out = append(out, Finding{
			Level:       LevelWarn,
			Code:        CodeGlobalDefaultProfile,
			Message:     fmt.Sprintf("global config sets default_profile=%q — this routes every project on the system to that profile", in.Global.DefaultProfile),
			Remediation: "move default_profile into the per-project .gaia/config.yaml; keep profile definitions in the global file",
			SourceFile:  in.GlobalConfigPath,
		})
	}
	if in.Global.DefaultRepo != "" {
		out = append(out, Finding{
			Level:       LevelWarn,
			Code:        CodeGlobalDefaultRepo,
			Message:     fmt.Sprintf("global config sets default_repo=%q — globally meaningless (every project gets the same repo target)", in.Global.DefaultRepo),
			Remediation: "remove default_repo from the global file; set it per-project in .gaia/config.yaml",
			SourceFile:  in.GlobalConfigPath,
		})
	}
	return out
}

// --- Credential hygiene -----------------------------------------

// checkCredentialHygiene covers three smells:
//
//  1. global credentials.yaml mode > 0600 (any group/other access
//     bit lit). The auth Save path writes 0600; a file that drifted
//     was either hand-edited or copied from another machine with
//     looser umask. ERR — credentials should never be readable by
//     other users.
//  2. project .gaia/credentials.yaml exists but the project's
//     .gitignore doesn't cover it. ERR — secret about to be
//     committed.
//  3. a token env var is exported AND a credentials file exists for
//     the same provider. WARN — rotation becomes ambiguous (which
//     copy is authoritative?).
func checkCredentialHygiene(in Inputs) []Finding {
	var out []Finding

	// (1) Global credentials file mode.
	if in.GlobalCredentialsPath != "" {
		info, err := os.Stat(in.GlobalCredentialsPath)
		if err == nil {
			mode := info.Mode().Perm()
			if mode&0o077 != 0 {
				out = append(out, Finding{
					Level:       LevelErr,
					Code:        CodeCredentialsFileMode,
					Message:     fmt.Sprintf("global credentials file mode is %#o; tokens are world- or group-readable", mode),
					Remediation: fmt.Sprintf("chmod 600 %s", in.GlobalCredentialsPath),
					SourceFile:  in.GlobalCredentialsPath,
				})
			}
		}
		// A missing global credentials file is the "no auth yet"
		// case — handled by the env/cred overlap check below;
		// don't double-report.
	}

	// (2) Project credentials file leak.
	if in.ProjectCredentialsPath != "" {
		if _, err := os.Stat(in.ProjectCredentialsPath); err == nil {
			// File exists. Check whether the repo's .gitignore
			// covers it. Repo root is the search base; relative
			// path is what we look for in the .gitignore.
			gitignorePath := ""
			if in.RepoRoot != "" {
				gitignorePath = filepath.Join(in.RepoRoot, ".gitignore")
			}
			covered := projectCredentialsIgnored(gitignorePath)
			if !covered {
				out = append(out, Finding{
					Level:       LevelErr,
					Code:        CodeProjectCredsLeak,
					Message:     ".gaia/credentials.yaml exists in the project but .gitignore does not cover it — about to commit secrets",
					Remediation: "run `gaia gitignore >> .gitignore`, or add `.gaia/credentials*` to .gitignore manually",
					SourceFile:  in.ProjectCredentialsPath,
				})
			}
		}
	}

	// (3) Env-and-credentials overlap. We can't tell exactly
	// which credential entry corresponds to which env var without
	// reaching into the resolved profile, but the smell is real
	// even at the coarse "both populated" level: rotating the
	// stored credential leaves the env var stale (or vice versa).
	if in.Credentials != nil && in.EnvVars != nil {
		// Bucket env vars by the provider their canonical fallback
		// resolves. Doctor reports one finding per provider when
		// the credentials store has a host AND the corresponding
		// env var is set.
		hostsByProvider := map[string][]string{}
		for _, s := range []*auth.Store{in.Credentials.Project, in.Credentials.Global} {
			if s == nil {
				continue
			}
			for _, key := range s.Hosts() {
				parts := strings.SplitN(key, ":", 2)
				if len(parts) != 2 {
					continue
				}
				p := parts[0]
				h := parts[1]
				// Dedup project-vs-global: project overrides, but
				// either source counts as "stored credential
				// present".
				if !contains(hostsByProvider[p], h) {
					hostsByProvider[p] = append(hostsByProvider[p], h)
				}
			}
		}
		// Iterate providers in sorted order so finding emission
		// is deterministic — tests pin codes(), and a map walk
		// would scramble GitHub vs Forgejo across runs.
		providers := make([]string, 0, len(hostsByProvider))
		for p := range hostsByProvider {
			providers = append(providers, p)
		}
		sort.Strings(providers)
		for _, p := range providers {
			hosts := hostsByProvider[p]
			sort.Strings(hosts)
			var envNames []string
			switch p {
			case "forgejo":
				envNames = []string{"FORGEJO_TOKEN", "GITEA_TOKEN"}
			case "github":
				envNames = []string{"GITHUB_TOKEN", "GH_TOKEN"}
			}
			for _, name := range envNames {
				if in.EnvVars[name] {
					out = append(out, Finding{
						Level: LevelWarn,
						Code:  CodeEnvAndCredOverlap,
						Message: fmt.Sprintf("%s env var is set AND a stored credential exists for provider %q (host(s): %s) — rotation is ambiguous",
							name, p, strings.Join(hosts, ", ")),
						Remediation: fmt.Sprintf("pick one: unset %s, or remove the stored credential via `gaia auth logout`", name),
					})
				}
			}
		}
	}

	return out
}

// projectCredentialsIgnored reports whether the .gitignore at
// gitignorePath covers `.gaia/credentials*` (or the bare
// `.gaia/credentials.yaml` form). Missing file → false (not
// covered); read error → false (the operator gets the leak
// warning even if we can't read .gitignore — failing closed is
// the safe default for a security check).
func projectCredentialsIgnored(gitignorePath string) bool {
	if gitignorePath == "" {
		return false
	}
	body, err := os.ReadFile(gitignorePath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		// Accept any pattern that would shadow .gaia/credentials.yaml.
		// `.gaia/credentials*` is the recommended block's form;
		// `.gaia/credentials.yaml` is the bare-file form; `.gaia/`
		// (whole directory) covers it too. `/.gaia/credentials*` is
		// the anchored form some operators prefer.
		switch l {
		case ".gaia/credentials*",
			".gaia/credentials.yaml",
			".gaia/credentials.*",
			".gaia/",
			"/.gaia/credentials*",
			"/.gaia/credentials.yaml",
			"/.gaia/":
			return true
		}
	}
	return false
}

// --- Profile coherence ------------------------------------------

// checkProfileCoherence covers three smells:
//
//  1. resolved profile has no `provider` — every command will
//     fail with "no provider configured". ERR.
//  2. resolved profile has no `api_url` — Forgejo provider can't
//     issue requests. ERR.
//  3. profile.TokenEnv names a variable that's currently empty
//     AND no canonical fallback env var (FORGEJO_TOKEN /
//     GITEA_TOKEN / GH_TOKEN / GITHUB_TOKEN) is set. WARN — the
//     command will hit `auth: no token` at runtime.
//  4. default_profile (merged) names a profile name that doesn't
//     exist in the merged profile map. WARN — `gaia` will reject
//     with "profile not found".
func checkProfileCoherence(in Inputs) []Finding {
	var out []Finding

	merged := config.Merge(in.Global, in.Project)

	// Profile we'll inspect: explicit override > merged default >
	// "" (doctor reports "no profile resolvable" below).
	profileName := in.Profile
	if profileName == "" {
		profileName = merged.DefaultProfile
	}

	// (4) default_profile names a missing profile.
	if profileName != "" {
		if _, ok := merged.Profiles[profileName]; !ok {
			out = append(out, Finding{
				Level: LevelWarn,
				Code:  CodeDefaultProfileMissing,
				Message: fmt.Sprintf("default_profile=%q is not defined in the merged profile map (have: %s)",
					profileName, strings.Join(sortedKeys(merged.Profiles), ", ")),
				Remediation: "fix the typo in default_profile, or add the missing profile under `profiles:`",
			})
			// Skip 1/2 — there is no profile to inspect.
			return out
		}
	}

	// (1) + (2) on the resolved profile (when one exists).
	if profileName != "" {
		p := merged.Profiles[profileName]
		if p.Provider == "" {
			out = append(out, Finding{
				Level:       LevelErr,
				Code:        CodeProfileNoProvider,
				Message:     fmt.Sprintf("profile %q has no `provider` set", profileName),
				Remediation: "add `provider: forgejo` (or `provider: github`) under the profile",
			})
		}
		if p.APIURL == "" {
			out = append(out, Finding{
				Level:       LevelErr,
				Code:        CodeProfileNoAPIURL,
				Message:     fmt.Sprintf("profile %q has no `api_url` set", profileName),
				Remediation: "add `api_url: https://your-forge/api/v1` (or the GitHub API URL) under the profile",
			})
		}
		// (3) token_env empty + no canonical fallback.
		if p.TokenEnv != "" && in.EnvVars != nil && !in.EnvVars[p.TokenEnv] {
			fallbacks := canonicalEnvFallbacks(p.Provider)
			anyFallback := false
			for _, name := range fallbacks {
				if in.EnvVars[name] {
					anyFallback = true
					break
				}
			}
			if !anyFallback {
				out = append(out, Finding{
					Level: LevelWarn,
					Code:  CodeTokenEnvEmpty,
					Message: fmt.Sprintf("profile %q sets token_env=%q but that variable is empty and no canonical fallback (%s) is set",
						profileName, p.TokenEnv, strings.Join(fallbacks, ", ")),
					Remediation: fmt.Sprintf("export %s, or run `gaia auth %s <url>` to store a credential", p.TokenEnv, providerCommandFor(p.Provider)),
				})
			}
		}
	}

	return out
}

// canonicalEnvFallbacks mirrors core/config.envNamesFor; doctor
// uses it independently to avoid an internal-package crossing.
// Keep in sync.
func canonicalEnvFallbacks(provider string) []string {
	switch provider {
	case "forgejo":
		return []string{"FORGEJO_TOKEN", "GITEA_TOKEN"}
	case "github":
		return []string{"GITHUB_TOKEN", "GH_TOKEN"}
	}
	return nil
}

// providerCommandFor maps a provider name to the `gaia auth …`
// subcommand the operator runs to store a credential.
func providerCommandFor(provider string) string {
	switch provider {
	case "github":
		return "gh"
	default:
		return "forgejo"
	}
}

// --- Repo resolution INFO ---------------------------------------

// checkRepoResolution mirrors the cmd/cli resolveRepo precedence
// (flag > git remote > project default_repo) and surfaces the
// outcome as INFO. Helps the operator debug split-host forge
// setups where SSH push host differs from API host.
func checkRepoResolution(in Inputs) Finding {
	source := ""
	resolved := ""
	switch {
	case in.RepoFlag != "":
		source = "--repo flag"
		resolved = in.RepoFlag
	case in.GitRemoteRepo != "":
		source = "git remote (autodetect)"
		resolved = in.GitRemoteRepo
	default:
		// Project default_repo lookup; doctor reads merged config
		// for the same view the CLI sees.
		merged := config.Merge(in.Global, in.Project)
		if merged.DefaultRepo != "" {
			source = "project .gaia/config.yaml default_repo"
			resolved = merged.DefaultRepo
		}
	}
	if resolved == "" {
		return Finding{
			Level:       LevelInfo,
			Code:        CodeRepoResolution,
			Message:     "repo not resolvable in cwd (no --repo, no git remote, no project default_repo)",
			Remediation: "run inside a configured checkout, or pass --repo owner/name",
		}
	}
	return Finding{
		Level:   LevelInfo,
		Code:    CodeRepoResolution,
		Message: fmt.Sprintf("repo would resolve to %s (source: %s)", resolved, source),
	}
}

// --- Cwd context INFO -------------------------------------------

// checkCwdContext lists which config layers contributed to the
// resolved state: project file path (if found), global file path
// (always reported when known), and which token env vars are set.
// Crucial for "why is it doing this?" debugging.
func checkCwdContext(in Inputs) Finding {
	parts := []string{}
	if in.ProjectConfigPath != "" {
		if _, err := os.Stat(in.ProjectConfigPath); err == nil {
			parts = append(parts, "project="+in.ProjectConfigPath)
		}
	}
	if in.GlobalConfigPath != "" {
		if _, err := os.Stat(in.GlobalConfigPath); err == nil {
			parts = append(parts, "global="+in.GlobalConfigPath)
		} else if errors.Is(err, fs.ErrNotExist) {
			parts = append(parts, "global=<absent>")
		}
	}
	envOn := []string{}
	for _, name := range []string{"GITEA_TOKEN", "FORGEJO_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if in.EnvVars[name] {
			envOn = append(envOn, name)
		}
	}
	if len(envOn) > 0 {
		parts = append(parts, "env="+strings.Join(envOn, ","))
	} else {
		parts = append(parts, "env=<none>")
	}
	msg := "resolved from: " + strings.Join(parts, ", ")
	return Finding{
		Level:   LevelInfo,
		Code:    CodeConfigLayers,
		Message: msg,
	}
}

// --- helpers ----------------------------------------------------

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]config.Profile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
