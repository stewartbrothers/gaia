package github

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

type apiReview struct {
	ID          int64     `json:"id"`
	User        apiUser   `json:"user"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
}

type apiInlineComment struct {
	ID        int64     `json:"id"`
	User      apiUser   `json:"user"`
	Body      string    `json:"body"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *apiReview) toType() types.Comment {
	return types.Comment{
		ID:        a.ID,
		Source:    "review",
		Author:    types.User{Login: a.User.Login},
		Body:      a.Body,
		CreatedAt: a.SubmittedAt,
		UpdatedAt: a.SubmittedAt,
	}
}

func (a *apiInlineComment) toType() types.Comment {
	return types.Comment{
		ID:        a.ID,
		Source:    "inline",
		Author:    types.User{Login: a.User.Login},
		Body:      a.Body,
		Path:      a.Path,
		Line:      a.Line,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

// ListComments returns the unified time-ordered comment stream for an
// issue or PR, drawing from up to three GitHub endpoints. Same shape
// as core/forgejo's ListComments — the trim-at-boundary contract from
// Provider.ListComments yields identical Comment values regardless of
// which forge served them.
func (p *Provider) ListComments(ctx context.Context, owner, repo string, n int, opts provider.ListCommentsOptions) ([]types.Comment, error) {
	want := func(s string) bool {
		if len(opts.Sources) == 0 {
			return true
		}
		for _, x := range opts.Sources {
			if x == s {
				return true
			}
		}
		return false
	}

	var out []types.Comment

	if want("issue") {
		comments, err := p.fetchIssueComments(ctx, owner, repo, n, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, comments...)
	}

	if want("review") {
		reviews, err := p.fetchPullReviews(ctx, owner, repo, n)
		if err == nil {
			for _, r := range reviews {
				if r.Body != "" {
					out = append(out, r.toType())
				}
			}
		} else if !isNotFound(err) {
			return nil, err
		}
	}

	if want("inline") {
		inline, err := p.fetchInlineComments(ctx, owner, repo, n)
		if err == nil {
			out = append(out, inline...)
		} else if !isNotFound(err) {
			return nil, err
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func (p *Provider) fetchPullReviews(ctx context.Context, owner, repo string, n int) ([]apiReview, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(envelopeDefaultLimit))
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews?%s", owner, repo, n, q.Encode())
	var raw []apiReview
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (p *Provider) fetchInlineComments(ctx context.Context, owner, repo string, n int) ([]types.Comment, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(envelopeDefaultLimit))
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments?%s", owner, repo, n, q.Encode())
	var raw []apiInlineComment
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]types.Comment, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, nil
}

func isNotFound(err error) bool {
	return exitcode.Of(err) == exitcode.NotFound
}

const envelopeDefaultLimit = 30
