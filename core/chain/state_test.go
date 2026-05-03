package chain_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/chain"
)

func TestNewTokenIsUnique(t *testing.T) {
	a, err := chain.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := chain.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("collision: both tokens are %q", a)
	}
	if len(a) != 32 {
		t.Errorf("token length: got %d, want 32 (16 bytes hex)", len(a))
	}
}

func TestStateFileEmptyArgsReturnEmpty(t *testing.T) {
	if got := chain.StateFile("", "x"); got != "" {
		t.Errorf("empty dir should return empty path; got %q", got)
	}
	if got := chain.StateFile("/tmp", ""); got != "" {
		t.Errorf("empty token should return empty path; got %q", got)
	}
}

func TestSaveAndLoadStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &chain.State{
		Token:         "abc123",
		CreatedAt:     time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC),
		YieldedAt:     time.Date(2026, 5, 3, 10, 5, 0, 0, time.UTC),
		YieldedAtStep: 1,
		YieldReason:   chain.YieldRateLimited,
		YieldPayload:  map[string]any{"retry_after": "60s"},
		Chain: chain.Chain{
			Name: "smoke",
			Steps: []chain.Step{
				{ID: "first", Run: "echo hi"},
				{ID: "second", Run: "exit 5"},
			},
		},
		Vars: map[string]string{"target": "main"},
		Captures: map[string]any{
			"first": "hi",
		},
		Steps: []chain.StepResult{
			{ID: "first", Status: chain.StepOK, ExitCode: 0, DurationMs: 5},
			{ID: "second", Status: chain.StepYielded, ExitCode: 5, DurationMs: 3},
		},
	}

	if err := chain.SaveState(dir, original); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Verify schema version got auto-set.
	if original.SchemaVersion != chain.CurrentSchemaVersion {
		t.Errorf("schema version not auto-set; got %d", original.SchemaVersion)
	}

	got, err := chain.LoadState(dir, "abc123")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.Token != "abc123" {
		t.Errorf("token: %q", got.Token)
	}
	if got.YieldReason != chain.YieldRateLimited {
		t.Errorf("yield reason: %q", got.YieldReason)
	}
	if got.YieldPayload["retry_after"] != "60s" {
		t.Errorf("payload: %+v", got.YieldPayload)
	}
	if len(got.Chain.Steps) != 2 || got.Chain.Steps[0].ID != "first" {
		t.Errorf("chain not preserved: %+v", got.Chain)
	}
	if got.Vars["target"] != "main" {
		t.Errorf("vars: %+v", got.Vars)
	}
	if got.Captures["first"] != "hi" {
		t.Errorf("captures: %+v", got.Captures)
	}
	if len(got.Steps) != 2 {
		t.Errorf("step results count: %d", len(got.Steps))
	}
}

func TestSaveStateUses0600(t *testing.T) {
	dir := t.TempDir()
	state := &chain.State{Token: "perm-test", Chain: chain.Chain{Name: "x"}}
	if err := chain.SaveState(dir, state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "perm-test.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode: got %o, want 0600", mode)
	}
}

func TestSaveStateRejectsEmptyToken(t *testing.T) {
	dir := t.TempDir()
	state := &chain.State{Chain: chain.Chain{Name: "x"}}
	err := chain.SaveState(dir, state)
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("expected empty-token error; got %v", err)
	}
}

func TestLoadStateMissingReturnsErrNotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := chain.LoadState(dir, "nope")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist; got %v", err)
	}
}

func TestLoadStateRejectsFutureSchema(t *testing.T) {
	dir := t.TempDir()
	// Hand-write a state file with a future schema version.
	body := "schema_version: 999\ntoken: future\nchain:\n  name: x\n  steps:\n    - id: a\n      run: echo\n"
	if err := os.WriteFile(filepath.Join(dir, "future.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := chain.LoadState(dir, "future")
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("expected schema-version error; got %v", err)
	}
}

func TestDeleteStateIdempotent(t *testing.T) {
	dir := t.TempDir()
	// First delete on an absent file should succeed silently.
	if err := chain.DeleteState(dir, "ghost"); err != nil {
		t.Errorf("missing file should not error; got %v", err)
	}
	// Now create + delete + delete again.
	state := &chain.State{Token: "real", Chain: chain.Chain{Name: "x"}}
	if err := chain.SaveState(dir, state); err != nil {
		t.Fatal(err)
	}
	if err := chain.DeleteState(dir, "real"); err != nil {
		t.Errorf("first delete: %v", err)
	}
	if err := chain.DeleteState(dir, "real"); err != nil {
		t.Errorf("second delete (idempotent): %v", err)
	}
}

func TestListStatesSortsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	for _, tok := range []string{"old", "newer", "newest"} {
		state := &chain.State{Token: tok, Chain: chain.Chain{Name: "x"}}
		if err := chain.SaveState(dir, state); err != nil {
			t.Fatal(err)
		}
		// Stagger mod times so the sort is deterministic.
		time.Sleep(20 * time.Millisecond)
	}

	infos, err := chain.ListStates(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 3 {
		t.Fatalf("count: %d", len(infos))
	}
	if infos[0].Token != "newest" || infos[2].Token != "old" {
		t.Errorf("sort order: %+v", infos)
	}
}

func TestListStatesIgnoresTempAndMissingDir(t *testing.T) {
	// Missing directory: no error, empty result.
	got, err := chain.ListStates(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Errorf("missing dir should not error; got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing dir should be empty; got %+v", got)
	}

	// Mixed-content directory: only .yaml files counted.
	dir := t.TempDir()
	state := &chain.State{Token: "real", Chain: chain.Chain{Name: "x"}}
	_ = chain.SaveState(dir, state)
	_ = os.WriteFile(filepath.Join(dir, ".chain-leftover.tmp"), []byte("partial"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("noise"), 0o600)

	got, _ = chain.ListStates(dir)
	if len(got) != 1 || got[0].Token != "real" {
		t.Errorf("filter failed: %+v", got)
	}
}

func TestCleanupStaleRemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	stalePath := filepath.Join(dir, "stale.yaml")
	freshPath := filepath.Join(dir, "fresh.yaml")
	_ = os.WriteFile(stalePath, []byte("stale"), 0o600)
	_ = os.WriteFile(freshPath, []byte("fresh"), 0o600)

	// Backdate stale's mtime so it's older than maxAge.
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := chain.CleanupStale(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("removed: got %d, want 1", removed)
	}
	if _, err := os.Stat(stalePath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stale should be gone; got err %v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Errorf("fresh should remain; got err %v", err)
	}
}

func TestDefaultStateDirHonorsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got, err := chain.DefaultStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/custom/state/gaia/chains" {
		t.Errorf("got %q", got)
	}
}

func TestDefaultStateDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/test")
	got, err := chain.DefaultStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/home/test/.local/state/gaia/chains" {
		t.Errorf("got %q", got)
	}
}
