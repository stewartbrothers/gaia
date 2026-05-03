package chain

import "github.com/stewartbrothers/gaia/core/exitcode"

// MapExitCode classifies a step's exit code into the chain
// yield-condition vocabulary. Every gaia exit code from
// `core/exitcode` is mapped; anything unrecognized falls back to
// `unknown_error` so default retry / `yield_on: [unknown_error]`
// catches it.
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
	case exitcode.MergeConflict:
		return YieldMergeConflict
	case exitcode.ReviewRequired:
		return YieldReviewRequired
	case exitcode.PolicyViolation:
		return YieldPolicyViolation
	case exitcode.CheckFailed:
		return YieldCheckFailed
	case exitcode.CheckFlaky:
		return YieldCheckFlaky
	default:
		return YieldUnknownError
	}
}
