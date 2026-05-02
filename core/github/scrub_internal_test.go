package github

import (
	"errors"
	"strings"
	"testing"
)

// scrubError is internal — tested in-package so the redaction
// invariant holds even when no transport error happens to surface
// the token in production. Tested independently of the client's
// error-wrapping pipeline so the rule "the token never reaches the
// caller" is enforced at its narrowest point.

func TestScrubErrorRedactsToken(t *testing.T) {
	in := errors.New("dial tcp: bearer ghp_secret123 timed out")
	out := scrubError(in, "ghp_secret123")
	if strings.Contains(out.Error(), "ghp_secret123") {
		t.Fatalf("token leaked: %q", out.Error())
	}
	if !strings.Contains(out.Error(), "<redacted>") {
		t.Errorf("expected <redacted> marker; got %q", out.Error())
	}
}

func TestScrubErrorPassThroughWhenNotPresent(t *testing.T) {
	in := errors.New("dial tcp: connection refused")
	out := scrubError(in, "ghp_secret123")
	// Same error returned (not re-wrapped) when the token isn't in
	// the message — keeps errors.Is/As behavior intact.
	if !errors.Is(out, in) {
		t.Errorf("expected pass-through; got new error %q", out.Error())
	}
}

func TestScrubErrorNilSafe(t *testing.T) {
	if scrubError(nil, "x") != nil {
		t.Error("nil err must pass through")
	}
}

func TestScrubErrorEmptyTokenSkipsScan(t *testing.T) {
	in := errors.New("anything")
	if scrubError(in, "") != in {
		t.Error("empty token must pass err through unchanged")
	}
}
