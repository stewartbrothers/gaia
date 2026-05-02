package github

import "context"

// Provider implements core/provider.Provider for GitHub. Methods are
// added in phases; commit 1 of the Phase 2 stack lands the foundation
// (Whoami) and the HTTP client. Subsequent commits add issues
// (#32), PRs (#33), diff (#34), and comments (#35).
//
// Each method delegates to Client and reuses the trim-at-boundary
// pattern from core/forgejo: an internal apiX shape decodes only the
// fields we need, then a toType() converter produces the trimmed
// core/types value.
type Provider struct {
	client *Client
}

// NewProvider builds a Provider over a freshly-constructed Client.
func NewProvider(opts Options) *Provider {
	return &Provider{client: New(opts)}
}

// apiCurrentUser is the shape of GET /user that we read.
type apiCurrentUser struct {
	Login string `json:"login"`
}

// Whoami returns the authenticated user's login. Foundational for the
// `gaia auth gh` validation flow and for the eventual `gaia whoami`
// against github.com.
func (p *Provider) Whoami(ctx context.Context) (string, error) {
	var u apiCurrentUser
	if err := p.client.Get(ctx, "/user", &u); err != nil {
		return "", err
	}
	return u.Login, nil
}
