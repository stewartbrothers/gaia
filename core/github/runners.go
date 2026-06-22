package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiRunner mirrors GitHub's self-hosted Actions-runner record. Labels
// are nested {name} objects on GitHub; flattened to []string at toType.
type apiRunner struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Status string `json:"status"`
	Busy   bool   `json:"busy"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (a *apiRunner) toType() types.Runner {
	labels := make([]string, 0, len(a.Labels))
	for _, l := range a.Labels {
		labels = append(labels, l.Name)
	}
	return types.Runner{Name: a.Name, Status: a.Status, Busy: a.Busy, Labels: labels}
}

// apiRunnersList is GitHub's list envelope (unlike Forgejo's bare array).
type apiRunnersList struct {
	TotalCount int         `json:"total_count"`
	Runners    []apiRunner `json:"runners"`
}

// ListRunners returns self-hosted Actions runner status for the repo, or
// the owner's org when opts.Org is set. GitHub wraps the array in
// {total_count, runners} and nests labels as {name} objects, which are
// flattened to a plain string slice.
func (p *Provider) ListRunners(ctx context.Context, owner, repo string, opts provider.ListRunnersOptions) ([]types.Runner, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	base := fmt.Sprintf("/repos/%s/%s/actions/runners", owner, repo)
	if opts.Org {
		base = fmt.Sprintf("/orgs/%s/actions/runners", owner)
	}
	path := base + "?" + q.Encode()

	var wrap apiRunnersList
	if err := p.client.Get(ctx, path, &wrap); err != nil {
		return nil, nil, err
	}
	out := make([]types.Runner, 0, len(wrap.Runners))
	for i := range wrap.Runners {
		out = append(out, wrap.Runners[i].toType())
	}
	return out, makePage(len(wrap.Runners), limit, opts.Cursor), nil
}
