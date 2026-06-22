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

// apiVariable mirrors GitHub's Actions-variable record. Unlike a
// secret, the value IS returned.
type apiVariable struct {
	Name      string     `json:"name"`
	Value     string     `json:"value"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

func (a *apiVariable) toType() types.Variable {
	return types.Variable{Name: a.Name, Value: a.Value, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

// apiVariablesList is GitHub's list envelope (unlike Forgejo's bare array).
type apiVariablesList struct {
	TotalCount int           `json:"total_count"`
	Variables  []apiVariable `json:"variables"`
}

// ListVariables returns Actions variables for the repo, or the owner's
// org when opts.Org is set. GitHub wraps the array in
// {total_count, variables}; the value IS returned.
func (p *Provider) ListVariables(ctx context.Context, owner, repo string, opts provider.ListVariablesOptions) ([]types.Variable, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	base := fmt.Sprintf("/repos/%s/%s/actions/variables", owner, repo)
	if opts.Org {
		base = fmt.Sprintf("/orgs/%s/actions/variables", owner)
	}
	path := base + "?" + q.Encode()

	var wrap apiVariablesList
	if err := p.client.Get(ctx, path, &wrap); err != nil {
		return nil, nil, err
	}
	out := make([]types.Variable, 0, len(wrap.Variables))
	for i := range wrap.Variables {
		out = append(out, wrap.Variables[i].toType())
	}
	return out, makePage(len(wrap.Variables), limit, opts.Cursor), nil
}
