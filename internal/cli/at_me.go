package cli

import (
	"context"

	"github.com/stewartbrothers/gaia/core/provider"
)

// atMe is the literal sentinel a caller passes as a flag value to
// mean "the configured user's login." resolveAtMe rewrites it in
// place. Matches the gh / tea convention so the muscle memory carries
// over. See issue #299.
const atMe = "@me"

// resolveAtMe rewrites every value pointed at by vals that equals
// atMe to the result of p.Whoami(ctx). The Whoami call is issued at
// most once per invocation, regardless of how many of the pointers
// point at "@me" — agents shouldn't pay a round-trip per flag.
//
// Pointers whose target isn't "@me" are left untouched; a nil pointer
// is skipped so callers can pass pointers unconditionally without
// nil-guarding each flag. If no value needs resolving, Whoami is not
// called at all and nil is returned.
func resolveAtMe(ctx context.Context, p provider.Provider, vals ...*string) error {
	needs := false
	for _, v := range vals {
		if v != nil && *v == atMe {
			needs = true
			break
		}
	}
	if !needs {
		return nil
	}
	login, err := p.Whoami(ctx)
	if err != nil {
		return err
	}
	for _, v := range vals {
		if v != nil && *v == atMe {
			*v = login
		}
	}
	return nil
}
