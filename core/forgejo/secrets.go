package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiSecret mirrors Forgejo's Actions-secret record. The API is
// write-only: a list returns the name and created_at, never the value.
type apiSecret struct {
	Name      string     `json:"name"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

func (a *apiSecret) toType() types.Secret {
	return types.Secret{Name: a.Name, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

// ListSecrets returns Actions secret metadata for the repo, or the
// owner's org when opts.Org is set. Forgejo returns a bare array; the
// secret value is never exposed by the API.
func (p *Provider) ListSecrets(ctx context.Context, owner, repo string, opts provider.ListSecretsOptions) ([]types.Secret, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	base := fmt.Sprintf("/repos/%s/%s/actions/secrets", owner, repo)
	if opts.Org {
		base = fmt.Sprintf("/orgs/%s/actions/secrets", owner)
	}
	path := base + "?" + q.Encode()

	var raw []apiSecret
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Secret, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}
