package forgejo

import (
	"context"
	"fmt"
	"strings"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiLabelFull mirrors Forgejo's full label record, including the ID
// (which we need for PATCH/DELETE — those endpoints take ID, not name)
// and the Color + Description fields the issue/PR responses don't
// bother with.
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
// case-insensitive name substring (opts.Name). Forgejo's labels
// endpoint isn't paginated for repo-level labels in practice, so we
// don't expose Page here. The filter runs client-side after the
// fetch — Forgejo's /labels takes no filter param (#328).
func (p *Provider) ListLabels(ctx context.Context, owner, repo string, opts provider.ListLabelsOptions) ([]types.Label, error) {
	raw, err := p.fetchLabels(ctx, owner, repo)
	if err != nil {
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
	path := fmt.Sprintf("/repos/%s/%s/labels", owner, repo)
	if err := p.client.Post(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// EditLabel patches a label by name. Forgejo's PATCH endpoint takes
// the label ID, so we do a list-and-find before issuing the PATCH.
// One extra round-trip per call; acceptable for a low-frequency
// operation. The list-and-find is shared with DeleteLabel.
func (p *Provider) EditLabel(ctx context.Context, owner, repo string, name string, opts provider.EditLabelOptions) (*types.Label, error) {
	id, err := p.findLabelIDByName(ctx, owner, repo, name)
	if err != nil {
		return nil, err
	}
	var raw apiLabelFull
	path := fmt.Sprintf("/repos/%s/%s/labels/%d", owner, repo, id)
	if err := p.client.Patch(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeleteLabel removes a label by name. Same lookup-by-name pattern
// as EditLabel.
func (p *Provider) DeleteLabel(ctx context.Context, owner, repo string, name string) error {
	id, err := p.findLabelIDByName(ctx, owner, repo, name)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/labels/%d", owner, repo, id)
	return p.client.Delete(ctx, path)
}

func (p *Provider) fetchLabels(ctx context.Context, owner, repo string) ([]apiLabelFull, error) {
	path := fmt.Sprintf("/repos/%s/%s/labels?limit=200", owner, repo)
	var raw []apiLabelFull
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (p *Provider) findLabelIDByName(ctx context.Context, owner, repo, name string) (int64, error) {
	all, err := p.fetchLabels(ctx, owner, repo)
	if err != nil {
		return 0, err
	}
	for _, l := range all {
		if l.Name == name {
			return l.ID, nil
		}
	}
	return 0, exitcode.Errorf(exitcode.NotFound, "label %q not found in %s/%s", name, owner, repo)
}
