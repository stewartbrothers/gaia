package auth_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	s, err := auth.Load(filepath.Join(t.TempDir(), "no-such.yaml"))
	if err != nil {
		t.Fatalf("Load(missing): %v", err)
	}
	if s == nil {
		t.Fatal("Load(missing) should return empty Store, not nil")
	}
	if got := s.Hosts(); len(got) != 0 {
		t.Errorf("hosts: got %v, want []", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.yaml")

	s := &auth.Store{}
	s.Set("forgejo", "git.example.com", auth.Credential{Token: "fjt", User: "alice"})
	s.Set("github", "github.com", auth.Credential{Token: "ght", User: "bob"})

	if err := auth.Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := auth.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, ok := loaded.Get("forgejo", "git.example.com")
	if !ok || c.Token != "fjt" || c.User != "alice" {
		t.Errorf("forgejo entry: got %+v ok=%v", c, ok)
	}
	c, ok = loaded.Get("github", "github.com")
	if !ok || c.Token != "ght" || c.User != "bob" {
		t.Errorf("github entry: got %+v ok=%v", c, ok)
	}
}

func TestSaveSetsRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.yaml")

	s := &auth.Store{}
	s.Set("forgejo", "git.example", auth.Credential{Token: "x"})
	if err := auth.Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm: got %o, want 0600", info.Mode().Perm())
	}
}

func TestStoreRemove(t *testing.T) {
	s := &auth.Store{}
	s.Set("forgejo", "host1", auth.Credential{Token: "a"})
	s.Set("forgejo", "host2", auth.Credential{Token: "b"})

	s.Remove("forgejo", "host1")
	if _, ok := s.Get("forgejo", "host1"); ok {
		t.Errorf("host1 should be removed")
	}
	if _, ok := s.Get("forgejo", "host2"); !ok {
		t.Errorf("host2 should still be present")
	}
}

func TestStoreRemoveMissingHostIsNoOp(t *testing.T) {
	s := &auth.Store{}
	s.Remove("forgejo", "nonexistent")
	s.Remove("github", "also-nonexistent")
	// No panic, no error to assert against — just that the calls returned.
}

func TestStoreHostsSorted(t *testing.T) {
	s := &auth.Store{}
	s.Set("forgejo", "z.example", auth.Credential{Token: "x"})
	s.Set("github", "a.example", auth.Credential{Token: "x"})
	s.Set("forgejo", "a.example", auth.Credential{Token: "x"})

	got := s.Hosts()
	want := []string{
		"forgejo:a.example",
		"forgejo:z.example",
		"github:a.example",
	}
	if len(got) != len(want) {
		t.Fatalf("count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStoreStringRedactsToken(t *testing.T) {
	s := &auth.Store{}
	s.Set("forgejo", "host", auth.Credential{Token: "very-secret"})

	s1 := s.String()
	if strings.Contains(s1, "very-secret") {
		t.Errorf("Store.String() leaked token: %q", s1)
	}
}

func TestCredentialStringRedactsToken(t *testing.T) {
	c := auth.Credential{Token: "very-secret", User: "alice"}

	for _, verb := range []string{"%s", "%v", "%+v", "%#v"} {
		got := fmt.Sprintf(verb, c)
		if strings.Contains(got, "very-secret") {
			t.Errorf("Credential format %q leaked token: %q", verb, got)
		}
		if !strings.Contains(got, "alice") {
			t.Errorf("Credential format %q should keep user: %q", verb, got)
		}
	}
}

func TestLoadInvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.yaml")
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Load(path); err == nil {
		t.Fatal("expected error on malformed YAML")
	}
}
