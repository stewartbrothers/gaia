package github

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// GitHub wikis live in a separate git repo at {owner}/{repo}.wiki.git
// rather than a REST endpoint, so a clone-cache implementation is
// needed before these methods can do real work. Issue #120 tracks
// that follow-up: shallow clone into ~/.cache/gaia/wikis/{owner}/{repo}/
// with TTL refresh, and back the same five Provider methods that
// Forgejo gets via REST in #108. Until that lands, every method here
// returns a clear NotImplemented error so callers fail fast with a
// pointer to the tracking issue.

const githubWikiNotImplementedMsg = "GitHub wikis require a clone-cache implementation, tracked in #120 (follow-up to #108)"

// ListWikiPages is not yet implemented on the GitHub provider.
func (p *Provider) ListWikiPages(_ context.Context, _, _ string, _ provider.ListWikiPagesOptions) ([]types.WikiPage, *provider.Page, error) {
	return nil, nil, exitcode.Errorf(exitcode.Generic, githubWikiNotImplementedMsg)
}

// GetWikiPage is not yet implemented on the GitHub provider.
func (p *Provider) GetWikiPage(_ context.Context, _, _, _ string) (*types.WikiPage, error) {
	return nil, exitcode.Errorf(exitcode.Generic, githubWikiNotImplementedMsg)
}

// SearchWikiPages is not yet implemented on the GitHub provider.
func (p *Provider) SearchWikiPages(_ context.Context, _, _, _ string, _ provider.SearchWikiOptions) ([]types.WikiSearchHit, error) {
	return nil, exitcode.Errorf(exitcode.Generic, githubWikiNotImplementedMsg)
}

// EditWikiPage is not yet implemented on the GitHub provider.
func (p *Provider) EditWikiPage(_ context.Context, _, _, _, _ string) (*types.WikiPage, error) {
	return nil, exitcode.Errorf(exitcode.Generic, githubWikiNotImplementedMsg)
}

// DeleteWikiPage is not yet implemented on the GitHub provider.
func (p *Provider) DeleteWikiPage(_ context.Context, _, _, _ string) error {
	return exitcode.Errorf(exitcode.Generic, githubWikiNotImplementedMsg)
}
