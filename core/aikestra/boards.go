package aikestra

import (
	"context"
	"net/url"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiBoard and apiStage are Aikestra's Board and Stage schemas. A
// single-board read fills each Stage's issues (open Issue numbers, in
// queue order); the board list leaves them out.
type apiBoard struct {
	Name    string     `json:"name"`
	Default bool       `json:"default"`
	Paused  bool       `json:"paused"`
	Stages  []apiStage `json:"stages"`
}

type apiStage struct {
	Name   string `json:"name"`
	Paused bool   `json:"paused"`
	Issues []int  `json:"issues"`
}

func (a apiBoard) toType() types.Board {
	out := types.Board{Name: a.Name, Default: a.Default, Paused: a.Paused, Stages: make([]types.Stage, len(a.Stages))}
	for i, s := range a.Stages {
		out.Stages[i] = types.Stage{Name: s.Name, Paused: s.Paused, Issues: s.Issues}
	}
	return out
}

// ListBoards lists the Project's boards.
func (p *Provider) ListBoards(ctx context.Context, owner string, opts provider.ListBoardsOptions) ([]types.Board, *provider.Page, error) {
	page, err := getPage[apiBoard](ctx, p.client, projectPath(owner, "/boards"), nil, opts.Limit, opts.Cursor)
	if err != nil {
		return nil, nil, err
	}
	out := make([]types.Board, len(page.Items))
	for i, b := range page.Items {
		out[i] = b.toType()
	}
	return out, pageOf(page.NextCursor), nil
}

// GetBoard fetches one board by name (case-insensitive) or ID.
func (p *Provider) GetBoard(ctx context.Context, owner, board string) (*types.Board, error) {
	var raw apiBoard
	if err := p.client.Get(ctx, projectPath(owner, "/boards/%s", url.PathEscape(board)), nil, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// MoveIssue places Issue n on a Stage. While an open Issue Blocks n the
// server refuses with 409 `blocked` (exit PolicyViolation) and a message
// naming the blockers, e.g. "Blocked by #1: …".
func (p *Provider) MoveIssue(ctx context.Context, owner, _ string, n int, opts provider.MoveIssueOptions) (*types.Issue, error) {
	var raw apiIssue
	if err := p.client.Put(ctx, issuePath(owner, n, "/placement"), opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}
