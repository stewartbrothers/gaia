package auth_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
)

func TestLayeredProjectWinsOverGlobal(t *testing.T) {
	g := &auth.Store{}
	g.Set("forgejo", "h1", auth.Credential{Token: "global", User: "g"})

	p := &auth.Store{}
	p.Set("forgejo", "h1", auth.Credential{Token: "project", User: "p"})

	l := &auth.Layered{Global: g, Project: p}
	c, src, ok := l.Get("forgejo", "h1")
	if !ok {
		t.Fatal("expected entry")
	}
	if c.Token != "project" || c.User != "p" {
		t.Errorf("expected project to win; got %+v", c)
	}
	if src != "project" {
		t.Errorf("source: got %q, want project", src)
	}
}

func TestLayeredFallsBackToGlobalWhenProjectMissesHost(t *testing.T) {
	g := &auth.Store{}
	g.Set("forgejo", "h1", auth.Credential{Token: "global"})
	p := &auth.Store{} // empty

	l := &auth.Layered{Global: g, Project: p}
	c, src, ok := l.Get("forgejo", "h1")
	if !ok || c.Token != "global" || src != "global" {
		t.Errorf("expected global fallback; got cred=%+v src=%q ok=%v", c, src, ok)
	}
}

func TestLayeredNotFound(t *testing.T) {
	l := &auth.Layered{}
	_, _, ok := l.Get("forgejo", "nope")
	if ok {
		t.Errorf("expected not-found")
	}
}

func TestProjectRootFindsGitDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := auth.ProjectRoot(sub); got != root {
		t.Errorf("ProjectRoot: got %q, want %q", got, root)
	}
}

func TestProjectRootReturnsEmptyOutsideRepo(t *testing.T) {
	root := t.TempDir() // no .git
	if got := auth.ProjectRoot(root); got != "" {
		t.Errorf("ProjectRoot outside repo: got %q, want empty", got)
	}
}

// TestProjectRootHandlesWorktreeGitFile pins the worktree-friendly
// behaviour: linked worktrees and submodules use a `.git` FILE
// (containing `gitdir: …`) rather than a directory. Treating only
// the directory shape as a project root silently breaks gaia inside
// a worktree (e.g. saved-chain resolution can't find .gaia/chains).
func TestProjectRootHandlesWorktreeGitFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/feat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := auth.ProjectRoot(sub); got != root {
		t.Errorf("worktree .git file should still resolve project root: got %q, want %q", got, root)
	}
}

func TestProjectPath(t *testing.T) {
	got := auth.ProjectPath("/some/repo")
	want := filepath.Join("/some/repo", ".gaia", "credentials.yaml")
	if got != want {
		t.Errorf("ProjectPath: got %q, want %q", got, want)
	}
}

func TestDefaultGlobalPathRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := auth.DefaultGlobalPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/xdg", "gaia", "credentials.yaml")
	if got != want {
		t.Errorf("with XDG: got %q, want %q", got, want)
	}
}

func TestDefaultGlobalPathFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/test")
	got, err := auth.DefaultGlobalPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/home/test", ".config", "gaia", "credentials.yaml")
	if got != want {
		t.Errorf("without XDG: got %q, want %q", got, want)
	}
}
