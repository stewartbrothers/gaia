// Package github: package-registry support is deliberately deferred
// to follow-up #121. This file wires the Provider methods up to a
// NotImplemented stub so the GitHub provider keeps satisfying the
// core.Provider interface while #107 lands list/view/delete for
// Forgejo only.
//
// The follow-up issue tracking real GitHub Packages support is
// referenced in each error string so an agent that runs
// `gaia packages list` against GitHub gets a clear pointer to where
// the work is being done. GitHub Packages has a per-registry surface
// (npm, maven, nuget, rubygems, container/GHCR, ...) that doesn't
// fit Forgejo's single-shape endpoint — the follow-up issue covers
// that design choice.
package github

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// ListPackages is not implemented for the GitHub provider; see #107
// follow-up for the dedicated GitHub Packages issue.
func (p *Provider) ListPackages(_ context.Context, _ string, _ provider.ListPackagesOptions) ([]types.Package, *provider.Page, error) {
	return nil, nil, errPackagesNotImplemented()
}

// GetPackage is not implemented for the GitHub provider.
func (p *Provider) GetPackage(_ context.Context, _, _, _, _ string) (*types.Package, error) {
	return nil, errPackagesNotImplemented()
}

// DeletePackage is not implemented for the GitHub provider.
func (p *Provider) DeletePackage(_ context.Context, _, _, _, _ string) error {
	return errPackagesNotImplemented()
}

// errPackagesNotImplemented is the single source of truth for the
// stub error message; tests assert on substring "not implemented" and
// "GitHub" so this stays grep-friendly.
func errPackagesNotImplemented() error {
	return exitcode.Errorf(exitcode.Generic,
		"GitHub Packages support is not implemented yet — tracked in #121 (follow-up to #107)")
}
