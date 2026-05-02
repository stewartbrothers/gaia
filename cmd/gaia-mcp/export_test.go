package main

import (
	"context"

	"github.com/stewartbrothers/gaia/core/provider"
)

// SetBuilderForTest replaces the package-level provider builder so
// MCP tool handler tests can substitute a fake provider for the real
// one. Pass nil to restore the production forgebuilder path.
//
// The fn signature returns provider.Provider (not the concrete
// *forgejo.Provider) so tests for the GitHub provider can also use
// this seam — same dispatch story as forgebuilder.Build. ctx is
// passed for tests that want to inspect per-request token plumbing.
func SetBuilderForTest(fn func(context.Context) (provider.Provider, error)) {
	if fn == nil {
		builderFn = defaultBuilder
		return
	}
	builderFn = fn
}
