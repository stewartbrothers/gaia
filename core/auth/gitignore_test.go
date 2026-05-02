package auth_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
)

func TestEnsureGitignoredCreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	if err := auth.EnsureGitignored(dir, ".gaia/credentials.yaml"); err != nil {
		t.Fatalf("EnsureGitignored: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(body), ".gaia/credentials.yaml") {
		t.Errorf(".gitignore should contain entry; got %q", string(body))
	}
}

func TestEnsureGitignoredIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := auth.EnsureGitignored(dir, ".gaia/credentials.yaml"); err != nil {
			t.Fatalf("EnsureGitignored #%d: %v", i, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	count := strings.Count(string(body), ".gaia/credentials.yaml")
	if count != 1 {
		t.Errorf("expected 1 occurrence after 3 calls; got %d in %q", count, string(body))
	}
}

func TestEnsureGitignoredAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	existing := "node_modules/\n*.log\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := auth.EnsureGitignored(dir, ".gaia/credentials.yaml"); err != nil {
		t.Fatalf("EnsureGitignored: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(body), "node_modules/") {
		t.Errorf("existing entries should be preserved; got %q", body)
	}
	if !strings.Contains(string(body), ".gaia/credentials.yaml") {
		t.Errorf("new entry should be appended; got %q", body)
	}
}

func TestEnsureGitignoredHandlesEntryAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	existing := "some-other\n.gaia/credentials.yaml\nthird-entry\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := auth.EnsureGitignored(dir, ".gaia/credentials.yaml"); err != nil {
		t.Fatalf("EnsureGitignored: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	count := strings.Count(string(body), ".gaia/credentials.yaml")
	if count != 1 {
		t.Errorf("should not duplicate existing entry; got %d in %q", count, body)
	}
}

func TestEnsureGitignoredHandlesTrailingNewlineCorrectly(t *testing.T) {
	dir := t.TempDir()
	// Existing file without trailing newline.
	existing := "node_modules/"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := auth.EnsureGitignored(dir, ".gaia/credentials.yaml"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	// Result should be "node_modules/\n.gaia/credentials.yaml\n" — both
	// entries on their own lines.
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "node_modules/" || lines[1] != ".gaia/credentials.yaml" {
		t.Errorf("expected two clean lines; got %q", string(body))
	}
}
