package chain_test

import (
	"testing"

	"github.com/stewartbrothers/gaia/core/chain"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

func TestMapExitCodeKnown(t *testing.T) {
	cases := []struct {
		exit int
		want chain.YieldCondition
	}{
		{exitcode.NotFound, chain.YieldNotFound},
		{exitcode.Auth, chain.YieldAuthError},
		{exitcode.RateLimit, chain.YieldRateLimited},
		{exitcode.Generic, chain.YieldUnknownError},
		{exitcode.Network, chain.YieldUnknownError}, // mapped to unknown for retry-policy alignment
	}
	for _, tc := range cases {
		if got := chain.MapExitCode(tc.exit); got != tc.want {
			t.Errorf("MapExitCode(%d): got %q, want %q", tc.exit, got, tc.want)
		}
	}
}

func TestMapExitCodeFallsBackToUnknown(t *testing.T) {
	// Any exit code we don't classify (e.g., a child command's own
	// custom code) becomes unknown_error so default retry handles it
	// and yield_on: [unknown_error] catches it.
	for _, e := range []int{42, 99, 127, 255} {
		if got := chain.MapExitCode(e); got != chain.YieldUnknownError {
			t.Errorf("MapExitCode(%d): expected unknown_error, got %q", e, got)
		}
	}
}

func TestMapExitCodeZeroReturnsUnknown(t *testing.T) {
	// Defensive: if a caller hands MapExitCode a 0 (which they
	// shouldn't — that's success), we don't crash, we return
	// unknown so accidental "yield_on: [unknown_error]" isn't
	// surprised.
	if got := chain.MapExitCode(0); got != chain.YieldUnknownError {
		t.Errorf("zero: got %q, want unknown_error", got)
	}
}
