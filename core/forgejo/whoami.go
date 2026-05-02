package forgejo

import "context"

type apiCurrentUser struct {
	Login string `json:"login"`
}

// Whoami returns the login of the authenticated user. Foundational
// for `gaia auth ...` (which calls Whoami to validate a freshly
// pasted PAT) and for `gaia whoami` (which proves the configured
// token still works).
func (p *Provider) Whoami(ctx context.Context) (string, error) {
	var u apiCurrentUser
	if err := p.client.Get(ctx, "/user", &u); err != nil {
		return "", err
	}
	return u.Login, nil
}
