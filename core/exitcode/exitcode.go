// Package exitcode defines gaia's CLI exit-code convention and the
// error type and helpers that producers (the Forgejo provider, the CLI
// wiring) use to surface a code through the error chain.
//
// Agents branch on these codes without parsing stderr. The full table
// is documented in docs/exit-codes.md; in short:
//
//	0   OK
//	1   Generic / unexpected
//	2   Usage / bad request shape
//	3   NotFound
//	4   Auth (401, 403)
//	5   RateLimit (429)
//	6   Network / transient (408, 5xx)
//	7   MergeConflict (PR merge 409)
//	8   ReviewRequired (branch protection: missing required reviews)
//	9   PolicyViolation (branch protection: other policy block, e.g.
//	    required-status-check missing)
//	10  CheckFailed (CI checks finished, ≥1 non-flaky failure)
//	11  CheckFlaky (CI checks finished, only flaky/retryable failures
//	    seen, OR `gaia pr ci-wait` reached its deadline while still
//	    pending — caller is expected to wait + retry)
//	12  NotImplemented (method exists on the Provider contract but
//	    this forge doesn't support it — caller should fall back, not
//	    retry; see docs/provider-contract.md §10)
//
// Codes 7–11 ship with chain Phase B-3 (#112) so chains can route on
// merge / CI / policy outcomes via structured `yield_on:` /
// `abort_on:` conditions rather than parsing free text. The chain
// vocabulary maps in core/chain/conditions.go (`MapExitCode`).
package exitcode

import (
	"errors"
	"fmt"
)

// Documented exit codes. New codes get appended; existing codes do not
// change value, ever — bump SchemaVersion and add a new code instead.
const (
	OK        = 0
	Generic   = 1
	Usage     = 2
	NotFound  = 3
	Auth      = 4
	RateLimit = 5
	Network   = 6
	// MergeConflict — PR merge endpoint returned 409 (or the upstream's
	// equivalent "head ref has diverged from base, can't fast-forward").
	// Maps to chain.YieldMergeConflict so a chain can pause + let the
	// agent push a rebase commit before resuming.
	MergeConflict = 7
	// ReviewRequired — write op blocked because the branch-protection
	// rule needs human review approvals that aren't present yet. Maps
	// to chain.YieldReviewRequired.
	ReviewRequired = 8
	// PolicyViolation — write op blocked by branch protection or repo
	// policy for a reason OTHER than missing reviews (e.g., a required
	// status check is missing or failed). Maps to chain.YieldPolicyViolation.
	PolicyViolation = 9
	// CheckFailed — `gaia pr ci-wait` saw at least one non-flaky CI
	// check fail. Maps to chain.YieldCheckFailed; agents typically
	// declare this in `abort_on:` because a real test failure
	// shouldn't be silently retried.
	CheckFailed = 10
	// CheckFlaky — `gaia pr ci-wait` either timed out while still
	// pending, or saw only flaky/transient failures (a check went
	// pending → failure → success during the window, or its name
	// matches a known retry-marker pattern). Maps to
	// chain.YieldCheckFlaky; agents typically declare this in
	// `yield_on:` so the chain can pause + the agent re-trigger CI.
	CheckFlaky = 11
	// NotImplemented — the method exists on the Provider contract but
	// this forge doesn't support it. Agents branch on this to offer a
	// fallback (the run's html_url instead of logs, a switch to the
	// other provider, etc.) — never to retry. Lands per #324; the
	// contract was always documented in docs/provider-contract.md §10
	// but used Generic until this code existed.
	NotImplemented = 12
)

// Error is the carrier type that lets a deep call site surface an exit
// code through the error chain. Producers use Errorf or Wrap; consumers
// (the CLI root command in #15) use Of.
type Error struct {
	Code    int
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *Error) Error() string {
	switch {
	case e.Cause != nil && e.Message != "":
		return e.Message + ": " + e.Cause.Error()
	case e.Cause != nil:
		return e.Cause.Error()
	default:
		return e.Message
	}
}

// Unwrap exposes the wrapped cause to errors.Is / errors.As / %w.
func (e *Error) Unwrap() error {
	return e.Cause
}

// Errorf builds a new Error with a formatted message. Use when you
// originate an exit-coded failure (no underlying cause to wrap).
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap attaches an exit code (and optional message prefix) to an
// existing error. The returned Error preserves the cause for errors.Is
// and errors.As walks.
func Wrap(err error, code int, message string) *Error {
	return &Error{Code: code, Message: message, Cause: err}
}

// Of returns the exit code for err. Walks the error chain via
// errors.As, returning the first *Error's code. nil → OK; any other
// non-coded error → Generic.
func Of(err error) int {
	if err == nil {
		return OK
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Generic
}

// FromHTTP translates an HTTP status code to a gaia exit code. Used by
// provider clients when the upstream replies with a non-2xx response.
func FromHTTP(status int) int {
	switch {
	case status >= 200 && status < 400:
		return OK
	case status == 401, status == 403:
		return Auth
	case status == 404:
		return NotFound
	case status == 408:
		return Network
	case status == 429:
		return RateLimit
	case status >= 500 && status < 600:
		return Network
	case status == 400, status == 422:
		return Usage
	}
	return Generic
}
