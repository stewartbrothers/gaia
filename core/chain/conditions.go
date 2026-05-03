package chain

import "github.com/stewartbrothers/gaia/core/exitcode"

// MapExitCode classifies a step's exit code into the chain
// yield-condition vocabulary. The first 5 conditions
// (auth_error, not_found, rate_limited, timeout, unknown_error)
// map directly from gaia's exitcode package; the rest
// (check_failed, merge_conflict, ...) require gaia commands to
// emit structured exits and ship in later phases.
//
// timeout is special: it isn't an exit code, it's "the runner
// killed the step because timeout: expired." Callers detect that
// out-of-band and pass YieldTimeout directly. MapExitCode never
// returns YieldTimeout.
//
// Design choice: the mapping is a pure function of exit code, not
// of stderr content. Parsing free text is fragile and forge-
// version-dependent; if a gaia command needs to communicate a
// richer category, it should ship a dedicated exit code.
func MapExitCode(exit int) YieldCondition {
	switch exit {
	case 0:
		// Success has no condition; caller shouldn't be classifying.
		// Return unknown_error as a defensive fallback.
		return YieldUnknownError
	case exitcode.Generic:
		return YieldUnknownError
	case exitcode.Usage:
		// Usage errors aren't really runtime conditions — operator
		// got the flags wrong. Treat as unknown for vocabulary
		// purposes; agents shouldn't be matching on Usage anyway.
		return YieldUnknownError
	case exitcode.NotFound:
		return YieldNotFound
	case exitcode.Auth:
		return YieldAuthError
	case exitcode.RateLimit:
		return YieldRateLimited
	case exitcode.Network:
		// Network errors are usually transient; map to unknown so the
		// default retry policy (B-2) picks them up. A dedicated
		// "network" condition would be useful but doesn't match the
		// existing alt-design vocabulary; revisit if there's demand.
		return YieldUnknownError
	default:
		return YieldUnknownError
	}
}
