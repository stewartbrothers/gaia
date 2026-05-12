package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// setupDoctorEnv pins HOME, XDG_CONFIG_HOME, repo-root, and the
// cwd so each test sees a deterministic config + credentials
// surface. Returns the resolved tempdir for the test to write
// fixture files into.
//
// Mirrors cmd/gaia/main_test.go's clean-env pattern so doctor's
// view doesn't accidentally pull in the developer's actual config.
func setupDoctorEnv(t *testing.T) (tempDir, configDir, credentialsPath string) {
	t.Helper()
	tempDir = t.TempDir()
	configDir = filepath.Join(tempDir, "config", "gaia")
	credentialsPath = filepath.Join(configDir, "credentials.yaml")
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "config"))
	for _, k := range []string{
		"FORGEJO_TOKEN", "FORGEJO_API_URL",
		"GITEA_TOKEN", "GITHUB_TOKEN", "GH_TOKEN",
		"GAIA_PROFILE", "GAIA_PROVIDER",
	} {
		t.Setenv(k, "")
	}
	// chdir to a non-git tempdir so the project-config layer is
	// silently absent — matching the golden-test harness.
	notGit := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(notGit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return tempDir, configDir, credentialsPath
}

// TestConfigDoctorCleanExitsZero — a pristine environment with no
// config and no credentials emits only INFO findings and exits 0.
func TestConfigDoctorCleanExitsZero(t *testing.T) {
	setupDoctorEnv(t)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "doctor"})

	if err := root.Execute(); err != nil {
		t.Errorf("execute: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "INFO repo-resolution") {
		t.Errorf("expected INFO repo-resolution; got: %s", out)
	}
	if !strings.Contains(out, "summary:") {
		t.Errorf("expected summary line; got: %s", out)
	}
}

// TestConfigDoctorGlobalDefaultProfileTriggersWARN — the smoking
// gun from #277. A global config with default_profile contaminates
// every project on the system; doctor must catch it.
func TestConfigDoctorGlobalDefaultProfileTriggersWARN(t *testing.T) {
	_, configDir, _ := setupDoctorEnv(t)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `default_profile: contaminator
profiles:
  contaminator:
    provider: forgejo
    api_url: https://example.com/api/v1
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "doctor"})

	if err := root.Execute(); err != nil {
		t.Errorf("execute: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "WARN global-default-profile") {
		t.Errorf("missing WARN global-default-profile; got: %s", stdout.String())
	}
}

// TestConfigDoctorStrictPromotesWarn — `--strict` flips every WARN
// to ERR so CI gating fires on smells, not just hard breakages.
func TestConfigDoctorStrictPromotesWarn(t *testing.T) {
	_, configDir, _ := setupDoctorEnv(t)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `default_repo: a/b
profiles: {}
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "doctor", "--strict"})

	err := root.Execute()
	if got := exitcode.Of(err); got == exitcode.OK {
		t.Errorf("--strict should exit non-zero on a WARN-only run; stdout: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "ERR global-default-repo") {
		t.Errorf("WARN not promoted to ERR; got: %s", stdout.String())
	}
}

// TestConfigDoctorQuietExitCodeOnly — `--quiet` suppresses output
// but exits non-zero when an ERR fires, so CI scripts can branch
// on $? without buffering output.
func TestConfigDoctorQuietExitCodeOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes only")
	}
	_, configDir, credPath := setupDoctorEnv(t)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// World-readable credentials file → ERR.
	if err := os.WriteFile(credPath, []byte("forgejo: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "doctor", "--quiet"})

	err := root.Execute()
	if got := exitcode.Of(err); got == exitcode.OK {
		t.Errorf("--quiet should exit non-zero on ERR; stdout: %s", stdout.String())
	}
	if got := stdout.String(); got != "" {
		t.Errorf("--quiet should produce no stdout; got: %s", got)
	}
}

// TestConfigDoctorFormatJSON — `--format json` returns the
// standard envelope with findings as data records.
func TestConfigDoctorFormatJSON(t *testing.T) {
	_, configDir, _ := setupDoctorEnv(t)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `default_profile: x
profiles:
  x:
    provider: forgejo
    api_url: https://example.com/api/v1
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "json", "config", "doctor"})

	if err := root.Execute(); err != nil {
		t.Errorf("execute: %v\nstderr: %s", err, stderr.String())
	}

	var env struct {
		SchemaVersion string `json:"schema_version"`
		Data          []struct {
			Level       string `json:"level"`
			Code        string `json:"code"`
			Message     string `json:"message"`
			Remediation string `json:"remediation"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\nstdout: %s", err, stdout.String())
	}
	if env.SchemaVersion == "" {
		t.Errorf("schema_version empty")
	}
	if len(env.Data) == 0 {
		t.Errorf("data empty; expected ≥1 finding")
	}
	// At least one finding should be the WARN we engineered.
	found := false
	for _, f := range env.Data {
		if f.Code == "global-default-profile" && f.Level == "WARN" {
			found = true
			if f.Remediation == "" {
				t.Errorf("remediation missing on global-default-profile")
			}
		}
	}
	if !found {
		t.Errorf("global-default-profile WARN not present in JSON: %+v", env.Data)
	}
}

// TestConfigDoctorERRExitsOne — any ERR finding exits 1 (Generic).
func TestConfigDoctorERRExitsOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes only")
	}
	_, configDir, credPath := setupDoctorEnv(t)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath, []byte("forgejo: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "doctor"})

	err := root.Execute()
	if got := exitcode.Of(err); got != exitcode.Generic {
		t.Errorf("exit code: got %d want %d (Generic)\nstderr: %s\nstdout: %s",
			got, exitcode.Generic, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "ERR credentials-file-mode") {
		t.Errorf("expected ERR credentials-file-mode; got: %s", stdout.String())
	}
}
