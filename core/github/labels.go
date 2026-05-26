package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiLabelFull is GitHub's full label record. Includes Color and
// Description (which the trimmed apiLabel used by issue/PR responses
// doesn't bother with).
type apiLabelFull struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func (a *apiLabelFull) toType() types.Label {
	return types.Label{
		ID:          a.ID,
		Name:        a.Name,
		Color:       a.Color,
		Description: a.Description,
	}
}

// ListLabels returns labels on the repo, optionally filtered by a
// case-insensitive name substring (opts.Name). GitHub's /labels has
// no wire-level filter param, so the filter runs client-side on the
// fetched catalog (#328).
func (p *Provider) ListLabels(ctx context.Context, owner, repo string, opts provider.ListLabelsOptions) ([]types.Label, error) {
	path := fmt.Sprintf("/repos/%s/%s/labels?per_page=200", owner, repo)
	var raw []apiLabelFull
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	needle := strings.ToLower(opts.Name)
	out := make([]types.Label, 0, len(raw))
	for i := range raw {
		if needle != "" && !strings.Contains(strings.ToLower(raw[i].Name), needle) {
			continue
		}
		out = append(out, raw[i].toType())
	}
	return out, nil
}

// CreateLabel makes a new label.
func (p *Provider) CreateLabel(ctx context.Context, owner, repo string, opts provider.CreateLabelOptions) (*types.Label, error) {
	var raw apiLabelFull
	if err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/labels", owner, repo), opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// EditLabel patches a label by name. Unlike Forgejo (which requires
// label ID), GitHub's endpoint takes the name in the URL — no
// list-then-PATCH dance needed.
func (p *Provider) EditLabel(ctx context.Context, owner, repo string, name string, opts provider.EditLabelOptions) (*types.Label, error) {
	var raw apiLabelFull
	path := fmt.Sprintf("/repos/%s/%s/labels/%s", owner, repo, url.PathEscape(name))
	if err := p.client.Patch(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeleteLabel removes a label by name.
func (p *Provider) DeleteLabel(ctx context.Context, owner, repo string, name string) error {
	path := fmt.Sprintf("/repos/%s/%s/labels/%s", owner, repo, url.PathEscape(name))
	return p.client.Delete(ctx, path)
}
