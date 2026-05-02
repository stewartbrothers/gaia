package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

func writeTokenFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	// WriteFile sometimes leaves group bits depending on umask; force.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return path
}

func TestLoadTokensHappyPath(t *testing.T) {
	path := writeTokenFile(t, `# comment line
tok_alice alice
tok_bob   bob's laptop

# another comment
tok_solo
`, 0o600)

	store, err := loadTokensFromFile(path)
	if err != nil {
		t.Fatalf("loadTokens: %v", err)
	}
	if len(store) != 3 {
		t.Fatalf("count: %d, want 3", len(store))
	}
	if store["tok_alice"] != "alice" {
		t.Errorf("alice label: %q", store["tok_alice"])
	}
	if store["tok_bob"] != "bob's laptop" {
		t.Errorf("bob label: %q", store["tok_bob"])
	}
	// Bare token gets a synthetic label so audit logs still attribute it.
	if !strings.HasPrefix(store["tok_solo"], "token-") {
		t.Errorf("solo label: %q (expected token-N)", store["tok_solo"])
	}
}

func TestLoadTokensRefusesGroupReadable(t *testing.T) {
	path := writeTokenFile(t, "tok_x alice\n", 0o640)
	_, err := loadTokensFromFile(path)
	if err == nil {
		t.Fatal("expected error for 0640 token file")
	}
	if exitcode.Of(err) != exitcode.Usage {
		t.Errorf("exit code: %d, want Usage", exitcode.Of(err))
	}
	if !strings.Contains(err.Error(), "permissive") {
		t.Errorf("error text: %q", err.Error())
	}
}

func TestLoadTokensRefusesWorldReadable(t *testing.T) {
	path := writeTokenFile(t, "tok_x alice\n", 0o644)
	_, err := loadTokensFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "permissive") {
		t.Errorf("expected permissive error; got %v", err)
	}
}

func TestLoadTokensRefusesEmpty(t *testing.T) {
	path := writeTokenFile(t, "# only comments\n\n# nothing else\n", 0o600)
	_, err := loadTokensFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "no tokens") {
		t.Errorf("expected 'no tokens' error; got %v", err)
	}
}

func TestLoadTokensRejectsDuplicates(t *testing.T) {
	path := writeTokenFile(t, "tok_dup alice\ntok_dup bob\n", 0o600)
	_, err := loadTokensFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error; got %v", err)
	}
}

func TestLoadTokensMissingFile(t *testing.T) {
	_, err := loadTokensFromFile("/nonexistent/path/tokens")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if exitcode.Of(err) != exitcode.Usage {
		t.Errorf("exit code: %d, want Usage", exitcode.Of(err))
	}
}

func TestLoadTokensEmptyPathReturnsNil(t *testing.T) {
	store, err := loadTokensFromFile("")
	if err != nil {
		t.Errorf("empty path must succeed silently; got %v", err)
	}
	if store != nil {
		t.Errorf("expected nil store; got %+v", store)
	}
}

func TestSplitTokenLine(t *testing.T) {
	cases := []struct {
		line, wantToken, wantLabel string
	}{
		{"tok_x alice", "tok_x", "alice"},
		{"tok_x  multi  word  label", "tok_x", "multi  word  label"},
		{"tok_x\talice", "tok_x", "alice"},
		{"tok_solo", "tok_solo", "token-7"},
		{"tok_x  ", "tok_x", "token-7"},
	}
	for _, tc := range cases {
		token, label := splitTokenLine(tc.line, 7)
		if token != tc.wantToken || label != tc.wantLabel {
			t.Errorf("%q: got (%q,%q), want (%q,%q)", tc.line, token, label, tc.wantToken, tc.wantLabel)
		}
	}
}
