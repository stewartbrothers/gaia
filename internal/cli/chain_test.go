package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/chain"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// helpers used by the resolveStateDir tests.

func mkdirAll(p string) error             { return os.MkdirAll(p, 0o700) }
func chtimes(p string, t time.Time) error { return os.Chtimes(p, t, t) }
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestParseVarFlags covers parseVarFlags' main shapes:
// happy path, multi-= values, missing =, bad keys, empty input.
func TestParseVarFlags(t *testing.T) {
	type want struct {
		out     map[string]string
		errCode int // 0 = expect nil err
		errSub  string
	}
	cases := []struct {
		name string
		in   []string
		want want
	}{
		{
			name: "empty input → empty map",
			in:   nil,
			want: want{out: map[string]string{}},
		},
		{
			name: "single key=value",
			in:   []string{"title=hello"},
			want: want{out: map[string]string{"title": "hello"}},
		},
		{
			name: "multiple key=value",
			in:   []string{"a=1", "b=2"},
			want: want{out: map[string]string{"a": "1", "b": "2"}},
		},
		{
			name: "key=val=ue (split on first =)",
			in:   []string{"msg=a=b=c"},
			want: want{out: map[string]string{"msg": "a=b=c"}},
		},
		{
			name: "empty value is allowed",
			in:   []string{"x="},
			want: want{out: map[string]string{"x": ""}},
		},
		{
			name: "missing = → usage error",
			in:   []string{"justakey"},
			want: want{errCode: exitcode.Usage, errSub: "must be key=value"},
		},
		{
			name: "leading = (empty key) → usage error",
			in:   []string{"=value"},
			want: want{errCode: exitcode.Usage, errSub: "must be key=value"},
		},
		{
			name: "bad key (starts with digit) → usage error",
			in:   []string{"1foo=bar"},
			want: want{errCode: exitcode.Usage, errSub: "must be"},
		},
		{
			name: "bad key (contains dash) → usage error",
			in:   []string{"foo-bar=baz"},
			want: want{errCode: exitcode.Usage, errSub: "must be"},
		},
		{
			// #154 regression: cobra StringSliceVar would split values
			// on commas, breaking JSON / list literals passed as a
			// single --var. StringArrayVar preserves them verbatim.
			// parseVarFlags itself never splits, but this case pins
			// the contract end-to-end (the slice gaia receives must
			// already contain unsplit values).
			name: "comma-bearing value preserved (no splitting)",
			in:   []string{"issues_json=[1,2,3]", "msg=a,b,c"},
			want: want{out: map[string]string{
				"issues_json": "[1,2,3]",
				"msg":         "a,b,c",
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cli.ParseVarFlagsForTest(tc.in)
			if tc.want.errCode != 0 {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				if exitcode.Of(err) != tc.want.errCode {
					t.Errorf("exit code: got %d want %d (%v)",
						exitcode.Of(err), tc.want.errCode, err)
				}
				if tc.want.errSub != "" && !strings.Contains(err.Error(), tc.want.errSub) {
					t.Errorf("error message %q missing substring %q", err.Error(), tc.want.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want.out) {
				t.Fatalf("len got=%d want=%d (got=%v)", len(got), len(tc.want.out), got)
			}
			for k, v := range tc.want.out {
				if got[k] != v {
					t.Errorf("key %q: got %q want %q", k, got[k], v)
				}
			}
		})
	}
}

// TestLooksLikeIdent covers the alpha/alphanum/underscore identifier
// predicate.
func TestLooksLikeIdent(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"a", true},
		{"A", true},
		{"_", true},
		{"foo", true},
		{"Foo_Bar", true},
		{"foo123", true},
		{"_x9", true},
		{"1foo", false}, // leading digit
		{"foo-bar", false},
		{"foo.bar", false},
		{"foo bar", false},
		{"foo$", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := cli.LooksLikeIdentForTest(tc.in); got != tc.want {
				t.Errorf("looksLikeIdent(%q) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestChainExitFromStatus checks that each Result.Status maps to the
// expected exit-code-bearing error (or nil).
func TestChainExitFromStatus(t *testing.T) {
	cases := []struct {
		name    string
		res     *chain.Result
		wantNil bool
		wantEC  int
		wantSub string
	}{
		{
			name:    "success → nil",
			res:     &chain.Result{Status: chain.StatusSuccess, Chain: "c"},
			wantNil: true,
		},
		{
			name:    "yielded → nil (chain alive, just paused)",
			res:     &chain.Result{Status: chain.StatusYielded, Chain: "c"},
			wantNil: true,
		},
		{
			name:    "failure → Generic with step",
			res:     &chain.Result{Status: chain.StatusFailure, Chain: "c", FailedStep: "boom"},
			wantEC:  exitcode.Generic,
			wantSub: "boom",
		},
		{
			name:    "aborted → Generic with reason",
			res:     &chain.Result{Status: chain.StatusAborted, Chain: "c", AbortReason: chain.YieldAuthError},
			wantEC:  exitcode.Generic,
			wantSub: "auth_error",
		},
		{
			name:    "unknown status → nil (defensive default)",
			res:     &chain.Result{Status: "weird", Chain: "c"},
			wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := cli.ChainExitFromStatusForTest(tc.res)
			if tc.wantNil {
				if err != nil {
					t.Errorf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if exitcode.Of(err) != tc.wantEC {
				t.Errorf("exit code: got %d want %d", exitcode.Of(err), tc.wantEC)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q missing substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestPrettyChainListEmpty / Multiple / Sort / BadType cover all
// branches of prettyChainList.
func TestPrettyChainList(t *testing.T) {
	t.Run("empty list prints placeholder", func(t *testing.T) {
		var buf bytes.Buffer
		if err := cli.PrettyChainListForTest(&buf, []chain.StateInfo{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "no yielded chains") {
			t.Errorf("expected placeholder message, got %q", buf.String())
		}
	})

	t.Run("multiple entries print one per line in supplied order", func(t *testing.T) {
		t1, _ := time.Parse(time.RFC3339, "2026-05-01T00:00:00Z")
		t2, _ := time.Parse(time.RFC3339, "2026-05-02T00:00:00Z")
		infos := []chain.StateInfo{
			{Token: "alpha", ModTime: t1},
			{Token: "beta", ModTime: t2},
		}
		var buf bytes.Buffer
		if err := cli.PrettyChainListForTest(&buf, infos); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
			t.Errorf("missing tokens in output: %q", got)
		}
		if !strings.Contains(got, "2026-05-01T00:00:00Z") {
			t.Errorf("missing first timestamp: %q", got)
		}
		// Order: prettyChainList iterates the slice as supplied —
		// callers (chain.ListStates) sort newest-first; we verify
		// the renderer preserves that.
		if strings.Index(got, "alpha") > strings.Index(got, "beta") {
			t.Errorf("entries reordered; want alpha before beta in input order: %q", got)
		}
	})

	t.Run("bad type rejected with error", func(t *testing.T) {
		var buf bytes.Buffer
		err := cli.PrettyChainListForTest(&buf, "not a slice")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unexpected type") {
			t.Errorf("error message: %q", err.Error())
		}
	})
}

// TestResolveStateDirXDG verifies XDG_STATE_HOME is honored and stale
// files are cleaned out.
func TestResolveStateDirXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("HOME", t.TempDir()) // make sure HOME fallback isn't used

	got, err := cli.ResolveStateDirForTest()
	if err != nil {
		t.Fatalf("resolveStateDir: %v", err)
	}
	want := filepath.Join(xdg, "gaia", "chains")
	if got != want {
		t.Errorf("dir: got %q want %q", got, want)
	}
}

// TestResolveStateDirHomeFallback verifies the $HOME/.local/state fallback
// when XDG_STATE_HOME is empty.
func TestResolveStateDirHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)

	got, err := cli.ResolveStateDirForTest()
	if err != nil {
		t.Fatalf("resolveStateDir: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "gaia", "chains")
	if got != want {
		t.Errorf("dir: got %q want %q", got, want)
	}
}

// TestResolveStateDirCleansStale verifies that resolveStateDir
// opportunistically removes stale state files via CleanupStale.
// We seed an old file into the dir, run resolveStateDir, and confirm
// it's gone.
func TestResolveStateDirCleansStale(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	// Seed a stale state file (mtime 30 days ago).
	dir := filepath.Join(xdg, "gaia", "chains")
	if err := mkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "stale.yaml")
	writeFile(t, stale, []byte("schema_version: 1\n"))
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := chtimes(stale, old); err != nil {
		t.Fatal(err)
	}

	if _, err := cli.ResolveStateDirForTest(); err != nil {
		t.Fatalf("resolveStateDir: %v", err)
	}

	if exists(stale) {
		t.Errorf("stale file %q should have been cleaned up", stale)
	}
}

// TestResolveStateDirHomeError exercises the error path when neither
// XDG_STATE_HOME nor HOME is resolvable. On most platforms unsetting
// HOME causes os.UserHomeDir to fail.
func TestResolveStateDirHomeError(t *testing.T) {
	// Skip if the platform allows UserHomeDir without HOME (it may
	// fall back to /etc/passwd lookup on some systems). The test is
	// best-effort: if it doesn't error, we don't fail.
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	dir, err := cli.ResolveStateDirForTest()
	if err == nil {
		// Some platforms still produce a path. Just ensure the
		// returned dir, if any, is not silently broken.
		if dir == "" {
			t.Error("got empty dir without error")
		}
		return
	}
	// If it errored, ensure it's the right exit-code shape and the
	// underlying cause is preserved so errors.Is/As can walk it.
	if exitcode.Of(err) != exitcode.Generic {
		t.Errorf("expected Generic exit code on home-resolve error; got %d", exitcode.Of(err))
	}
	var unwrapped *exitcode.Error
	if !errors.As(err, &unwrapped) {
		t.Fatal("expected wrapped *exitcode.Error")
	}
}

// TestChainRunVarFlagPreservesCommas pins the cobra-flag binding for
// `gaia chain run --var`: it must be StringArrayVar (which preserves
// commas) not StringSliceVar (which splits on them). #154.
//
// A future revert that swaps the flag type back to StringSliceVar
// would trip this test — the cobra Value.Type() string is the
// contract.
func TestChainRunVarFlagPreservesCommas(t *testing.T) {
	root := cli.NewRootCmd()
	chainCmd, _, err := root.Find([]string{"chain", "run"})
	if err != nil {
		t.Fatalf("find chain run cmd: %v", err)
	}
	flag := chainCmd.Flags().Lookup("var")
	if flag == nil {
		t.Fatal("chain run has no --var flag")
	}
	if got := flag.Value.Type(); got != "stringArray" {
		t.Errorf("--var flag type: got %q want %q (StringSliceVar splits commas; StringArrayVar preserves them; #154)",
			got, "stringArray")
	}

	// Same check on chain resume --modify-vars: the Phase B-2 modify
	// directive accepts vars too and shares the comma-preservation
	// requirement.
	resumeCmd, _, err := root.Find([]string{"chain", "resume"})
	if err != nil {
		t.Fatalf("find chain resume cmd: %v", err)
	}
	mvFlag := resumeCmd.Flags().Lookup("modify-vars")
	if mvFlag == nil {
		t.Fatal("chain resume has no --modify-vars flag")
	}
	if got := mvFlag.Value.Type(); got != "stringArray" {
		t.Errorf("--modify-vars flag type: got %q want %q (#154)",
			got, "stringArray")
	}
}
