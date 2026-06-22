package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiRunner mirrors Forgejo's self-hosted Actions-runner record. Labels
// is decoded into runnerLabels, which tolerates both wire shapes Forgejo
// has emitted across versions — a bare ["self-hosted"] string array and
// an object array [{"name":"self-hosted"}].
type apiRunner struct {
	Name   string       `json:"name"`
	Status string       `json:"status"`
	Busy   bool         `json:"busy"`
	Labels runnerLabels `json:"labels"`
}

func (a *apiRunner) toType() types.Runner {
	return types.Runner{Name: a.Name, Status: a.Status, Busy: a.Busy, Labels: a.Labels}
}

// runnerLabels flattens either of Forgejo's label encodings to a plain
// string slice. UnmarshalJSON probes the first non-whitespace byte after
// the opening bracket: a quote means a string array, a brace means an
// object array of {name}.
type runnerLabels []string

func (l *runnerLabels) UnmarshalJSON(data []byte) error {
	// Try the plain string-array shape first.
	var strs []string
	if err := json.Unmarshal(data, &strs); err == nil {
		*l = strs
		return nil
	}
	// Fall back to the object-array shape [{name}].
	var objs []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &objs); err != nil {
		return err
	}
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.Name)
	}
	*l = out
	return nil
}

// ListRunners returns self-hosted Actions runner status for the repo, or
// the owner's org when opts.Org is set. Forgejo returns a bare array; the
// repo-level list may be empty when runners are org/instance-scoped.
func (p *Provider) ListRunners(ctx context.Context, owner, repo string, opts provider.ListRunnersOptions) ([]types.Runner, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	base := fmt.Sprintf("/repos/%s/%s/actions/runners", owner, repo)
	if opts.Org {
		base = fmt.Sprintf("/orgs/%s/actions/runners", owner)
	}
	path := base + "?" + q.Encode()

	var raw []apiRunner
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Runner, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}
