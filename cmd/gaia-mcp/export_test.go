package main

import "github.com/stewartbrothers/gaia/core/forgejo"

// SetBuilderForTest replaces the package-level provider builder so
// MCP tool handler tests can substitute an httptest-backed Forgejo
// provider for the real one. Pass nil to restore the production
// forgebuilder path.
func SetBuilderForTest(fn func() (*forgejo.Provider, error)) {
	if fn == nil {
		builderFn = defaultBuilder
		return
	}
	builderFn = fn
}
