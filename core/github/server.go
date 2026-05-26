package github

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/types"
)

// ServerVersion is not implemented for GitHub.com, which has no public
// version endpoint. GitHub Enterprise Server exposes /api/v3/meta but
// that shape differs; add per-kind dispatch if GHES support is needed.
func (p *Provider) ServerVersion(_ context.Context) (*types.ServerVersion, error) {
	return nil, exitcode.Errorf(exitcode.NotImplemented,
		"server version is not available for the GitHub provider")
}
