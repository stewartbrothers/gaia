package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiSecret mirrors GitHub's Actions-secret record. The API is
// write-only: a list returns the name and created_at/updated_at, never
// the value.
type apiSecret struct {
	Name      string     `json:"name"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

func (a *apiSecret) toType() types.Secret {
	return types.Secret{Name: a.Name, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

// apiSecretsList is GitHub's list envelope (unlike Forgejo's bare array).
type apiSecretsList struct {
	TotalCount int         `json:"total_count"`
	Secrets    []apiSecret `json:"secrets"`
}

// ListSecrets returns Actions secret metadata for the repo, or the
// owner's org when opts.Org is set. GitHub wraps the array in
// {total_count, secrets}; the secret value is never exposed by the API.
func (p *Provider) ListSecrets(ctx context.Context, owner, repo string, opts provider.ListSecretsOptions) ([]types.Secret, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	base := fmt.Sprintf("/repos/%s/%s/actions/secrets", owner, repo)
	if opts.Org {
		base = fmt.Sprintf("/orgs/%s/actions/secrets", owner)
	}
	path := base + "?" + q.Encode()

	var wrap apiSecretsList
	if err := p.client.Get(ctx, path, &wrap); err != nil {
		return nil, nil, err
	}
	out := make([]types.Secret, 0, len(wrap.Secrets))
	for i := range wrap.Secrets {
		out = append(out, wrap.Secrets[i].toType())
	}
	return out, makePage(len(wrap.Secrets), limit, opts.Cursor), nil
}
