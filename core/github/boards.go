package github

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// GitHub has no kanban boards or "Do Now" priority flag; both are
// Aikestra concepts (#404). The registration declares CapBoards and
// CapIssuePriority unsupported, so the CLI and MCP surfaces hide them;
// these stubs keep the Provider contract satisfied for direct callers.

// ListBoards is not implemented for GitHub.
func (p *Provider) ListBoards(_ context.Context, _ string, _ provider.ListBoardsOptions) ([]types.Board, *provider.Page, error) {
	return nil, nil, errNoBoards()
}

// GetBoard is not implemented for GitHub.
func (p *Provider) GetBoard(_ context.Context, _, _ string) (*types.Board, error) {
	return nil, errNoBoards()
}

// MoveIssue is not implemented for GitHub.
func (p *Provider) MoveIssue(_ context.Context, _, _ string, _ int, _ provider.MoveIssueOptions) (*types.Issue, error) {
	return nil, errNoBoards()
}

// SetIssuePriority is not implemented for GitHub.
func (p *Provider) SetIssuePriority(_ context.Context, _, _ string, _ int, _ bool) (*types.Issue, error) {
	return nil, exitcode.Errorf(exitcode.NotImplemented,
		"issue priority is not available for the github provider")
}

func errNoBoards() error {
	return exitcode.Errorf(exitcode.NotImplemented,
		"boards are not available for the github provider")
}
