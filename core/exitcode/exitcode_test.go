package exitcode_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

func TestConstantsHaveDocumentedValues(t *testing.T) {
	// Documented in docs/exit-codes.md. Agents branch on these — drift
	// is a wire-shape break.
	cases := map[string]struct {
		got, want int
	}{
		"OK":              {exitcode.OK, 0},
		"Generic":         {exitcode.Generic, 1},
		"Usage":           {exitcode.Usage, 2},
		"NotFound":        {exitcode.NotFound, 3},
		"Auth":            {exitcode.Auth, 4},
		"RateLimit":       {exitcode.RateLimit, 5},
		"Network":         {exitcode.Network, 6},
		"MergeConflict":   {exitcode.MergeConflict, 7},
		"ReviewRequired":  {exitcode.ReviewRequired, 8},
		"PolicyViolation": {exitcode.PolicyViolation, 9},
		"CheckFailed":     {exitcode.CheckFailed, 10},
		"CheckFlaky":      {exitcode.CheckFlaky, 11},
		"NotImplemented":  {exitcode.NotImplemented, 12},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %d, want %d", name, c.got, c.want)
		}
	}
}

func TestOfNilIsOK(t *testing.T) {
	if got := exitcode.Of(nil); got != exitcode.OK {
		t.Errorf("Of(nil): got %d, want OK", got)
	}
}

func TestOfPlainErrorIsGeneric(t *testing.T) {
	err := errors.New("something went wrong")
	if got := exitcode.Of(err); got != exitcode.Generic {
		t.Errorf("Of(plain): got %d, want Generic", got)
	}
}

func TestOfErrorReturnsItsCode(t *testing.T) {
	err := exitcode.Errorf(exitcode.NotFound, "issue %d not found", 42)
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("Of(NotFound err): got %d, want NotFound", got)
	}
}

func TestOfWalksWrappedChain(t *testing.T) {
	inner := exitcode.Errorf(exitcode.Auth, "401 from upstream")
	wrapped := fmt.Errorf("forgejo client: %w", inner)
	doublyWrapped := fmt.Errorf("provider: %w", wrapped)
	if got := exitcode.Of(doublyWrapped); got != exitcode.Auth {
		t.Errorf("Of(deeply wrapped): got %d, want Auth", got)
	}
}

func TestErrorMessageFormatsCleanly(t *testing.T) {
	err := exitcode.Errorf(exitcode.NotFound, "issue %d not found", 42)
	want := "issue 42 not found"
	if err.Error() != want {
		t.Errorf("Error(): got %q, want %q", err.Error(), want)
	}
}

func TestErrorMessageWithCausePrefixes(t *testing.T) {
	cause := errors.New("connection refused")
	err := exitcode.Wrap(cause, exitcode.Network, "fetch")
	want := "fetch: connection refused"
	if err.Error() != want {
		t.Errorf("Error() with cause+message: got %q, want %q", err.Error(), want)
	}
}

func TestErrorMessageOnlyCauseNoPrefix(t *testing.T) {
	cause := errors.New("connection refused")
	err := exitcode.Wrap(cause, exitcode.Network, "")
	want := "connection refused"
	if err.Error() != want {
		t.Errorf("Error() with cause only: got %q, want %q", err.Error(), want)
	}
}

func TestWrapPreservesCauseAndCode(t *testing.T) {
	cause := errors.New("dial: connection refused")
	err := exitcode.Wrap(cause, exitcode.Network, "fetch issues")
	if exitcode.Of(err) != exitcode.Network {
		t.Errorf("wrapped code: got %d, want Network", exitcode.Of(err))
	}
	if !errors.Is(err, cause) {
		t.Errorf("wrapped error should still match cause via errors.Is")
	}
	if !errors.As(err, new(*exitcode.Error)) {
		t.Errorf("wrapped error should still be exitcode.Error via errors.As")
	}
}

func TestFromHTTPMaps(t *testing.T) {
	cases := []struct {
		status int
		want   int
	}{
		{200, exitcode.OK},
		{204, exitcode.OK},
		{301, exitcode.OK}, // 3xx is success-ish from CLI POV
		{400, exitcode.Usage},
		{401, exitcode.Auth},
		{403, exitcode.Auth},
		{404, exitcode.NotFound},
		{408, exitcode.Network}, // request timeout = transient
		{422, exitcode.Usage},
		{429, exitcode.RateLimit},
		{500, exitcode.Network},
		{502, exitcode.Network},
		{503, exitcode.Network},
		{504, exitcode.Network},
	}
	for _, c := range cases {
		if got := exitcode.FromHTTP(c.status); got != c.want {
			t.Errorf("FromHTTP(%d): got %d, want %d", c.status, got, c.want)
		}
	}
}

func TestFromHTTPDefaultsToGenericForUnknown(t *testing.T) {
	if got := exitcode.FromHTTP(418); got != exitcode.Generic {
		t.Errorf("FromHTTP(418 teapot): got %d, want Generic", got)
	}
}

func TestErrorIsTargetCode(t *testing.T) {
	// Two errors with the same code are NOT the same error; errors.Is
	// against an *exitcode.Error checks identity, not code. This test
	// pins the documented behavior so a future "make codes comparable"
	// change has to be deliberate.
	a := exitcode.Errorf(exitcode.Auth, "first")
	b := exitcode.Errorf(exitcode.Auth, "second")
	if errors.Is(a, b) {
		t.Errorf("two distinct Auth errors should NOT be errors.Is-equal")
	}
}
