package forgejo

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// Wiki support for the Forgejo provider lands in commit 2 of #108.
// This commit only adds the interface methods and types so the rest
// of the module compiles; the real implementation against the
// /repos/{owner}/{repo}/wiki/* endpoints follows immediately.

// ListWikiPages will return wiki page summaries for the repo.
func (p *Provider) ListWikiPages(_ context.Context, _, _ string, _ provider.ListWikiPagesOptions) ([]types.WikiPage, *provider.Page, error) {
	return nil, nil, exitcode.Errorf(exitcode.Generic, "forgejo: ListWikiPages not implemented yet (lands in #108 commit 2)")
}

// GetWikiPage will fetch one wiki page by slug.
func (p *Provider) GetWikiPage(_ context.Context, _, _, _ string) (*types.WikiPage, error) {
	return nil, exitcode.Errorf(exitcode.Generic, "forgejo: GetWikiPage not implemented yet (lands in #108 commit 2)")
}

// SearchWikiPages will perform client-side title + body matching.
func (p *Provider) SearchWikiPages(_ context.Context, _, _, _ string, _ provider.SearchWikiOptions) ([]types.WikiSearchHit, error) {
	return nil, exitcode.Errorf(exitcode.Generic, "forgejo: SearchWikiPages not implemented yet (lands in #108 commit 2)")
}

// EditWikiPage will upsert a wiki page (create or replace).
func (p *Provider) EditWikiPage(_ context.Context, _, _, _, _ string) (*types.WikiPage, error) {
	return nil, exitcode.Errorf(exitcode.Generic, "forgejo: EditWikiPage not implemented yet (lands in #108 commit 2)")
}

// DeleteWikiPage will remove a wiki page by slug.
func (p *Provider) DeleteWikiPage(_ context.Context, _, _, _ string) error {
	return exitcode.Errorf(exitcode.Generic, "forgejo: DeleteWikiPage not implemented yet (lands in #108 commit 2)")
}
