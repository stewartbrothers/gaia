package forgejo

import (
	"context"

	"github.com/stewartbrothers/gaia/core/types"
)

// ServerVersion returns the Forgejo instance's version string.
// Hits GET /version on the configured API host.
func (p *Provider) ServerVersion(ctx context.Context) (*types.ServerVersion, error) {
	var raw struct {
		Version string `json:"version"`
	}
	if err := p.client.Get(ctx, "/version", &raw); err != nil {
		return nil, err
	}
	return &types.ServerVersion{Version: raw.Version}, nil
}
