package aikestra

import (
	"context"
	"fmt"
	"net/url"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

var _ provider.Provider = (*Provider)(nil)

// Provider implements core/provider.Provider for Aikestra. Methods for
// resources API v1 doesn't have live in unsupported.go and return
// exitcode.NotImplemented.
//
// The read cache is not wired for Aikestra yet: every call goes
// upstream.
type Provider struct {
	client *Client
}

// NewProvider builds a Provider over a freshly-constructed Client.
func NewProvider(opts Options) *Provider {
	return &Provider{client: New(opts)}
}

// apiUser is GET /user. Principal is "user" or "delegated-agent" (an
// agent acting for the User); either way the identity is the User's.
type apiUser struct {
	Principal string `json:"principal"`
	Email     string `json:"email"`
	Name      string `json:"name"`
}

// Whoami returns the authenticated User's email — Aikestra identifies
// people by email everywhere (assignees, authors).
func (p *Provider) Whoami(ctx context.Context) (string, error) {
	var u apiUser
	if err := p.client.Get(ctx, "/user", nil, &u); err != nil {
		return "", err
	}
	return u.Email, nil
}

// ServerVersion reports the instance's public API version, read from the
// OpenAPI document (`info.version`), which needs no token. Aikestra has
// no separate product-version endpoint.
func (p *Provider) ServerVersion(ctx context.Context) (*types.ServerVersion, error) {
	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := p.client.Get(ctx, "/openapi.json", nil, &doc); err != nil {
		return nil, err
	}
	return &types.ServerVersion{Version: doc.Info.Version}, nil
}

// projectPath is the path of a Project-scoped resource. owner is the
// Project slug.
func projectPath(owner, format string, args ...any) string {
	return "/projects/" + url.PathEscape(owner) + fmt.Sprintf(format, args...)
}

// issuePath is the path of issue n, or of a sub-resource under it.
// Issues belong to the Project, so the repo is not part of the path.
func issuePath(owner string, n int, suffix string) string {
	return projectPath(owner, "/issues/%d%s", n, suffix)
}

func notImplemented(what string) error {
	return exitcode.Errorf(exitcode.NotImplemented,
		"%s is not available for the aikestra provider (not in Aikestra API v1)", what)
}
