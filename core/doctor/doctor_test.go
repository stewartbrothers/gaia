package doctor_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/config"
	"github.com/stewartbrothers/gaia/core/doctor"
)

// findByCode returns the first finding with the given code, or nil.
func findByCode(t *testing.T, fs []doctor.Finding, code string) *doctor.Finding {
	t.Helper()
	for i := range fs {
		if fs[i].Code == code {
			return &fs[i]
		}
	}
	return nil
}

// codes returns the sorted list of codes emitted by a run; tests
// use this for "set equality" assertions that don't care about
// order.
func codes(fs []doctor.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Code)
	}
	sort.Strings(out)
	return out
}

// hasCode reports whether any finding in fs uses code.
func hasCode(fs []doctor.Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

// --- Multi-project safety ---------------------------------------

func TestGlobalDefaultProfileTriggersWarn(t *testing.T) {
	in := doctor.Inputs{
		Global: &config.Config{
			DefaultProfile: "personal",
			Profiles: map[string]config.Profile{
				"personal": {Provider: "forgejo", APIURL: "https://example.com/api/v1"},
			},
		},
		GlobalConfigPath: "/home/u/.config/gaia/config.yaml",
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeGlobalDefaultProfile)
	if f == nil {
		t.Fatalf("missing finding %s; got codes: %v", doctor.CodeGlobalDefaultProfile, codes(got))
	}
	if f.Level != doctor.LevelWarn {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelWarn)
	}
	if f.SourceFile != in.GlobalConfigPath {
		t.Errorf("source_file: got %q want %q", f.SourceFile, in.GlobalConfigPath)
	}
	if f.Remediation == "" {
		t.Error("remediation empty; doctor's contract is to include one")
	}
}

func TestGlobalDefaultRepoTriggersWarn(t *testing.T) {
	in := doctor.Inputs{
		Global: &config.Config{DefaultRepo: "owner/repo"},
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeGlobalDefaultRepo)
	if f == nil {
		t.Fatalf("missing %s; got %v", doctor.CodeGlobalDefaultRepo, codes(got))
	}
	if f.Level != doctor.LevelWarn {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelWarn)
	}
}

func TestCleanGlobalNoMultiProjectFindings(t *testing.T) {
	in := doctor.Inputs{
		Global: &config.Config{
			Profiles: map[string]config.Profile{
				"x": {Provider: "forgejo", APIURL: "https://example.com/api/v1"},
			},
		},
	}
	got := doctor.Run(in)
	if hasCode(got, doctor.CodeGlobalDefaultProfile) {
		t.Errorf("clean global emitted %s", doctor.CodeGlobalDefaultProfile)
	}
	if hasCode(got, doctor.CodeGlobalDefaultRepo) {
		t.Errorf("clean global emitted %s", doctor.CodeGlobalDefaultRepo)
	}
}

// --- Credential hygiene -----------------------------------------

func TestCredentialsFileModeERR(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.yaml")
	if err := os.WriteFile(path, []byte("forgejo:\n  host: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := doctor.Inputs{GlobalCredentialsPath: path}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeCredentialsFileMode)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeCredentialsFileMode, codes(got))
	}
	if f.Level != doctor.LevelErr {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelErr)
	}
	if !doctor.HasErrors(got) {
		t.Error("HasErrors false on a file-mode ERR")
	}
}

func TestCredentialsFileModeClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.yaml")
	if err := os.WriteFile(path, []byte("forgejo:\n  host: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := doctor.Inputs{GlobalCredentialsPath: path}
	got := doctor.Run(in)
	if hasCode(got, doctor.CodeCredentialsFileMode) {
		t.Errorf("0600 file flagged: %v", codes(got))
	}
}

func TestProjectCredentialsLeakERR(t *testing.T) {
	dir := t.TempDir()
	gaiaDir := filepath.Join(dir, ".gaia")
	if err := os.MkdirAll(gaiaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(gaiaDir, "credentials.yaml")
	if err := os.WriteFile(credPath, []byte("forgejo:\n  host: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// .gitignore present but does NOT cover .gaia/credentials*
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("# only\nnode_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := doctor.Inputs{
		ProjectCredentialsPath: credPath,
		RepoRoot:               dir,
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeProjectCredsLeak)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeProjectCredsLeak, codes(got))
	}
	if f.Level != doctor.LevelErr {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelErr)
	}
}

func TestProjectCredentialsGitignored(t *testing.T) {
	dir := t.TempDir()
	gaiaDir := filepath.Join(dir, ".gaia")
	if err := os.MkdirAll(gaiaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(gaiaDir, "credentials.yaml")
	if err := os.WriteFile(credPath, []byte("forgejo:\n  host: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".gaia/credentials*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := doctor.Inputs{
		ProjectCredentialsPath: credPath,
		RepoRoot:               dir,
	}
	got := doctor.Run(in)
	if hasCode(got, doctor.CodeProjectCredsLeak) {
		t.Errorf("covered .gitignore still flagged: %v", codes(got))
	}
}

func TestEnvAndCredentialsOverlapWarn(t *testing.T) {
	store := newStore(t, "forgejo", "example.com", "tok-redacted")
	in := doctor.Inputs{
		Credentials: &auth.Layered{Global: store},
		EnvVars:     map[string]bool{"GITEA_TOKEN": true},
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeEnvAndCredOverlap)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeEnvAndCredOverlap, codes(got))
	}
	if f.Level != doctor.LevelWarn {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelWarn)
	}
	// Defence-in-depth: the message must not echo any token value
	// (the env var only carries a bool in Inputs, but pin the
	// contract anyway).
	if strings.Contains(f.Message, "tok-redacted") {
		t.Errorf("token value leaked into message: %s", f.Message)
	}
}

func TestEnvAndCredentialsNoOverlapWhenEnvUnset(t *testing.T) {
	store := newStore(t, "forgejo", "example.com", "tok")
	in := doctor.Inputs{
		Credentials: &auth.Layered{Global: store},
		EnvVars:     map[string]bool{},
	}
	got := doctor.Run(in)
	if hasCode(got, doctor.CodeEnvAndCredOverlap) {
		t.Errorf("env unset still flagged overlap: %v", codes(got))
	}
}

// --- Profile coherence ------------------------------------------

func TestProfileMissingProviderERR(t *testing.T) {
	in := doctor.Inputs{
		Global: &config.Config{
			Profiles: map[string]config.Profile{
				"p": {APIURL: "https://example.com/api/v1"},
			},
		},
		Profile: "p",
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeProfileNoProvider)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeProfileNoProvider, codes(got))
	}
	if f.Level != doctor.LevelErr {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelErr)
	}
}

func TestProfileMissingAPIURLERR(t *testing.T) {
	in := doctor.Inputs{
		Global: &config.Config{
			Profiles: map[string]config.Profile{
				"p": {Provider: "forgejo"},
			},
		},
		Profile: "p",
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeProfileNoAPIURL)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeProfileNoAPIURL, codes(got))
	}
	if f.Level != doctor.LevelErr {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelErr)
	}
}

func TestTokenEnvEmptyWarn(t *testing.T) {
	in := doctor.Inputs{
		Global: &config.Config{
			Profiles: map[string]config.Profile{
				"p": {
					Provider: "forgejo",
					APIURL:   "https://example.com/api/v1",
					TokenEnv: "MY_FORGE_TOKEN",
				},
			},
		},
		Profile: "p",
		EnvVars: map[string]bool{},
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeTokenEnvEmpty)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeTokenEnvEmpty, codes(got))
	}
	if f.Level != doctor.LevelWarn {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelWarn)
	}
}

func TestTokenEnvEmptyButFallbackPresentNoWarn(t *testing.T) {
	in := doctor.Inputs{
		Global: &config.Config{
			Profiles: map[string]config.Profile{
				"p": {
					Provider: "forgejo",
					APIURL:   "https://example.com/api/v1",
					TokenEnv: "MY_FORGE_TOKEN",
				},
			},
		},
		Profile: "p",
		EnvVars: map[string]bool{"GITEA_TOKEN": true},
	}
	got := doctor.Run(in)
	if hasCode(got, doctor.CodeTokenEnvEmpty) {
		t.Errorf("fallback present still warned: %v", codes(got))
	}
}

func TestDefaultProfileMissingWarn(t *testing.T) {
	in := doctor.Inputs{
		Global: &config.Config{
			DefaultProfile: "ghost",
			Profiles: map[string]config.Profile{
				"real": {Provider: "forgejo", APIURL: "https://example.com/api/v1"},
			},
		},
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeDefaultProfileMissing)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeDefaultProfileMissing, codes(got))
	}
	if f.Level != doctor.LevelWarn {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelWarn)
	}
}

// --- Repo + cwd context -----------------------------------------

func TestRepoResolutionFromFlag(t *testing.T) {
	in := doctor.Inputs{RepoFlag: "alpha/beta"}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeRepoResolution)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeRepoResolution, codes(got))
	}
	if !strings.Contains(f.Message, "alpha/beta") {
		t.Errorf("message lacks resolved repo: %s", f.Message)
	}
	if !strings.Contains(f.Message, "--repo") {
		t.Errorf("message lacks --repo source attribution: %s", f.Message)
	}
}

func TestRepoResolutionFromGitRemote(t *testing.T) {
	in := doctor.Inputs{GitRemoteRepo: "x/y"}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeRepoResolution)
	if f == nil {
		t.Fatal("missing repo-resolution finding")
	}
	if !strings.Contains(f.Message, "x/y") {
		t.Errorf("message lacks resolved repo: %s", f.Message)
	}
	if !strings.Contains(f.Message, "git remote") {
		t.Errorf("message lacks git-remote attribution: %s", f.Message)
	}
}

func TestRepoResolutionFromProjectDefault(t *testing.T) {
	in := doctor.Inputs{
		Project: &config.Config{DefaultRepo: "p/q"},
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeRepoResolution)
	if f == nil {
		t.Fatal("missing repo-resolution finding")
	}
	if !strings.Contains(f.Message, "p/q") {
		t.Errorf("message lacks resolved repo: %s", f.Message)
	}
}

func TestRepoResolutionUnresolvable(t *testing.T) {
	in := doctor.Inputs{}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeRepoResolution)
	if f == nil {
		t.Fatal("missing repo-resolution finding")
	}
	if !strings.Contains(f.Message, "not resolvable") {
		t.Errorf("expected 'not resolvable' message: %s", f.Message)
	}
}

func TestCwdContextLists(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(globalPath, []byte("profiles: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := doctor.Inputs{
		GlobalConfigPath: globalPath,
		EnvVars:          map[string]bool{"GITEA_TOKEN": true},
	}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeConfigLayers)
	if f == nil {
		t.Fatal("missing config-layers finding")
	}
	if !strings.Contains(f.Message, "global=") {
		t.Errorf("config-layers missing global=: %s", f.Message)
	}
	if !strings.Contains(f.Message, "GITEA_TOKEN") {
		t.Errorf("config-layers missing env: %s", f.Message)
	}
}

// --- Workflows precedence footgun -------------------------------

// writeWorkflows is a test helper that creates the named workflow
// files under <root>/<dir>/. dir is e.g. ".forgejo/workflows" or
// ".github/workflows".
func writeWorkflows(t *testing.T, root, dir string, names ...string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(dir))
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(full, n), []byte("on: push\njobs: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkflowsShadowedWarn(t *testing.T) {
	dir := t.TempDir()
	// .github defines the real gate; .forgejo has one unrelated file
	// that silently disables it.
	writeWorkflows(t, dir, ".github/workflows", "ci.yml", "deploy.yml")
	writeWorkflows(t, dir, ".forgejo/workflows", "lint.yml")
	in := doctor.Inputs{RepoRoot: dir}
	got := doctor.Run(in)
	f := findByCode(t, got, doctor.CodeWorkflowsShadowed)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeWorkflowsShadowed, codes(got))
	}
	if f.Level != doctor.LevelWarn {
		t.Errorf("level: got %s want %s", f.Level, doctor.LevelWarn)
	}
	if f.Remediation == "" {
		t.Error("remediation empty; doctor's contract is to include one")
	}
	// Message should name the shadowed workflows so the operator can
	// act without re-investigating.
	if !strings.Contains(f.Message, "ci.yml") || !strings.Contains(f.Message, "deploy.yml") {
		t.Errorf("message lacks shadowed names: %s", f.Message)
	}
}

func TestWorkflowsShadowedStrictPromotesToErr(t *testing.T) {
	dir := t.TempDir()
	writeWorkflows(t, dir, ".github/workflows", "ci.yml")
	writeWorkflows(t, dir, ".forgejo/workflows", "other.yml")
	got := doctor.PromoteWarnings(doctor.Run(doctor.Inputs{RepoRoot: dir}))
	f := findByCode(t, got, doctor.CodeWorkflowsShadowed)
	if f == nil {
		t.Fatalf("missing finding %s; got: %v", doctor.CodeWorkflowsShadowed, codes(got))
	}
	if f.Level != doctor.LevelErr {
		t.Errorf("--strict should promote to ERR; got %s", f.Level)
	}
}

func TestWorkflowsOnlyForgejoNoFinding(t *testing.T) {
	dir := t.TempDir()
	writeWorkflows(t, dir, ".forgejo/workflows", "ci.yml")
	got := doctor.Run(doctor.Inputs{RepoRoot: dir})
	if hasCode(got, doctor.CodeWorkflowsShadowed) {
		t.Errorf(".forgejo-only flagged: %v", codes(got))
	}
}

func TestWorkflowsOnlyGithubNoFinding(t *testing.T) {
	dir := t.TempDir()
	writeWorkflows(t, dir, ".github/workflows", "ci.yml")
	got := doctor.Run(doctor.Inputs{RepoRoot: dir})
	if hasCode(got, doctor.CodeWorkflowsShadowed) {
		t.Errorf(".github-only flagged: %v", codes(got))
	}
}

func TestWorkflowsForgejoCoversAllGithubNoFinding(t *testing.T) {
	dir := t.TempDir()
	// .forgejo mirrors (and exceeds) the .github set — nothing is
	// shadowed, so no finding.
	writeWorkflows(t, dir, ".github/workflows", "ci.yml", "deploy.yml")
	writeWorkflows(t, dir, ".forgejo/workflows", "ci.yml", "deploy.yml", "extra.yml")
	got := doctor.Run(doctor.Inputs{RepoRoot: dir})
	if hasCode(got, doctor.CodeWorkflowsShadowed) {
		t.Errorf(".forgejo covers all .github names but still flagged: %v", codes(got))
	}
}

func TestWorkflowsBothDirsEmptyNoFinding(t *testing.T) {
	dir := t.TempDir()
	// Both directories exist but neither holds a workflow file.
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".forgejo", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := doctor.Run(doctor.Inputs{RepoRoot: dir})
	if hasCode(got, doctor.CodeWorkflowsShadowed) {
		t.Errorf("empty dirs flagged: %v", codes(got))
	}
}

func TestWorkflowsNoRepoRootNoFinding(t *testing.T) {
	// Not inside a checkout — doctor can't inspect the filesystem.
	got := doctor.Run(doctor.Inputs{})
	if hasCode(got, doctor.CodeWorkflowsShadowed) {
		t.Errorf("empty RepoRoot flagged: %v", codes(got))
	}
}

func TestWorkflowsCaseInsensitiveAndYamlExt(t *testing.T) {
	dir := t.TempDir()
	// Forgejo treats .yml and .yaml alike, and the name match should
	// be case-insensitive. .forgejo/CI.yaml covers .github/ci.yml.
	writeWorkflows(t, dir, ".github/workflows", "ci.yml")
	writeWorkflows(t, dir, ".forgejo/workflows", "CI.yaml")
	got := doctor.Run(doctor.Inputs{RepoRoot: dir})
	if hasCode(got, doctor.CodeWorkflowsShadowed) {
		t.Errorf("case/ext-equivalent coverage still flagged: %v", codes(got))
	}
}

func TestWorkflowsNonYamlFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	// Only non-workflow files in .github/workflows → nothing real is
	// shadowed.
	writeWorkflows(t, dir, ".github/workflows", "README.md")
	writeWorkflows(t, dir, ".forgejo/workflows", "ci.yml")
	got := doctor.Run(doctor.Inputs{RepoRoot: dir})
	if hasCode(got, doctor.CodeWorkflowsShadowed) {
		t.Errorf("non-yaml .github files flagged: %v", codes(got))
	}
}

// --- Exit-code helpers + strict promotion -----------------------

func TestHasErrors(t *testing.T) {
	fs := []doctor.Finding{
		{Level: doctor.LevelInfo, Code: "x"},
		{Level: doctor.LevelWarn, Code: "y"},
	}
	if doctor.HasErrors(fs) {
		t.Error("HasErrors true with no ERR")
	}
	fs = append(fs, doctor.Finding{Level: doctor.LevelErr, Code: "z"})
	if !doctor.HasErrors(fs) {
		t.Error("HasErrors false with one ERR")
	}
}

func TestPromoteWarningsPromotesAll(t *testing.T) {
	fs := []doctor.Finding{
		{Level: doctor.LevelInfo, Code: "i"},
		{Level: doctor.LevelWarn, Code: "w1"},
		{Level: doctor.LevelWarn, Code: "w2"},
		{Level: doctor.LevelErr, Code: "e"},
	}
	out := doctor.PromoteWarnings(fs)
	for _, f := range out {
		if f.Code == "w1" || f.Code == "w2" {
			if f.Level != doctor.LevelErr {
				t.Errorf("%s not promoted: %s", f.Code, f.Level)
			}
		}
	}
	if !doctor.HasErrors(out) {
		t.Error("HasErrors false after promotion")
	}
}

// --- helpers ----------------------------------------------------

// newStore returns a single-entry auth.Store using the package's
// Set helper so the internal map is initialized correctly.
func newStore(t *testing.T, provider, host, token string) *auth.Store {
	t.Helper()
	s := &auth.Store{}
	s.Set(provider, host, auth.Credential{APIURL: "https://" + host + "/api/v1", Token: token})
	return s
}
