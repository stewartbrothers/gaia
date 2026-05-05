package autodetect_test

import (
	"os/exec"
	"testing"

	"github.com/stewartbrothers/gaia/core/autodetect"
)

func gitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestFromGitRemoteSSHForm(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "remote", "add", "origin", "git@forge.example.com:myorg/myrepo.git")

	got, err := autodetect.FromGitRemote(dir, "")
	if err != nil {
		t.Fatalf("FromGitRemote: %v", err)
	}
	if got.Host != "forge.example.com" || got.Owner != "myorg" || got.Name != "myrepo" {
		t.Errorf("got %+v", got)
	}
}

func TestFromGitRemoteHTTPSForm(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/foo/bar.git")

	got, err := autodetect.FromGitRemote(dir, "")
	if err != nil {
		t.Fatalf("FromGitRemote: %v", err)
	}
	if got.Host != "github.com" || got.Owner != "foo" || got.Name != "bar" {
		t.Errorf("got %+v", got)
	}
}

func TestFromGitRemoteNamedRemote(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:other/repo.git")
	runGit(t, dir, "remote", "add", "upstream", "git@github.com:upstream/repo.git")

	got, err := autodetect.FromGitRemote(dir, "upstream")
	if err != nil {
		t.Fatalf("FromGitRemote: %v", err)
	}
	if got.Owner != "upstream" {
		t.Errorf("named remote: got %+v, want owner=upstream", got)
	}
}

func TestFromGitRemoteNotARepo(t *testing.T) {
	gitOrSkip(t)
	if _, err := autodetect.FromGitRemote(t.TempDir(), ""); err == nil {
		t.Fatal("expected error from non-repo dir; got nil")
	}
}

func TestFromGitRemoteNoOriginRemote(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	if _, err := autodetect.FromGitRemote(dir, ""); err == nil {
		t.Fatal("expected error from repo with no origin; got nil")
	}
}
