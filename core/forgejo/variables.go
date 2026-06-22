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

// apiVariable mirrors Forgejo's Actions-variable record. Unlike a
// secret, the value IS returned — but the field is named `data`, not
// `value`.
type apiVariable struct {
	Name      string     `json:"name"`
	Data      string     `json:"data"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

func (a *apiVariable) toType() types.Variable {
	return types.Variable{Name: a.Name, Value: a.Data, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

// ListVariables returns Actions variables for the repo, or the owner's
// org when opts.Org is set. Forgejo returns a bare array; the value
// lives in the `data` field.
func (p *Provider) ListVariables(ctx context.Context, owner, repo string, opts provider.ListVariablesOptions) ([]types.Variable, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	base := fmt.Sprintf("/repos/%s/%s/actions/variables", owner, repo)
	if opts.Org {
		base = fmt.Sprintf("/orgs/%s/actions/variables", owner)
	}
	path := base + "?" + q.Encode()

	var raw []apiVariable
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Variable, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}
