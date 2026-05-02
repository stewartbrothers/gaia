// Package exitcode defines gaia's CLI exit-code convention and the
// error type and helpers that producers (the Forgejo provider, the CLI
// wiring) use to surface a code through the error chain.
//
// Agents branch on these codes without parsing stderr. The full table
// is documented in docs/exit-codes.md; in short:
//
//	0  OK
//	1  Generic / unexpected
//	2  Usage / bad request shape
//	3  NotFound
//	4  Auth (401, 403)
//	5  RateLimit (429)
//	6  Network / transient (408, 5xx)
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
