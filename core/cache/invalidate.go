package cache

import (
	"context"
)

// Invalidator is a thin convenience wrapper around a [Cache] that
// centralises the "what to evict on each mutation" decisions. The
// helpers here are intentionally tolerant of a nil cache: every
// provider's write method can call `cache.NewInvalidator(c).EditIssue(...)`
// even when caching is off, and the invocation is a no-op.
//
// Eviction policy (see #42):
//
//   - Single-resource mutations (EditIssue, EditPullRequest, etc.)
//     evict the matching object key AND every list_index row for the
//     repo. Lists are cheap to recompute.
//   - Resource-creation mutations (CreateIssue) only evict the
//     list_index rows; there's no pre-existing object row to drop.
//   - Delete mutations (DeleteRelease, DeleteWebhook) evict both
//     the object and the list_index rows.
//
// # Note (#158)
//
// In #152 this struct held a *Cache (the SQLite-backed concrete type
// at the time). After #158 it holds the [Cache] interface so the
// downstream provider packages don't pull the SQLite driver into
// their test binaries.
type Invalidator struct {
	cache Cache
}

// NewInvalidator returns an Invalidator backed by c. nil c is fine;
// every method is a no-op in that case. Use:
//
//	cache.NewInvalidator(p.client.cache).AfterCreate(ctx, ...)
//
// from provider write paths. The helper centralises the "is the
// cache nil?" check so call sites stay short.
func NewInvalidator(c Cache) *Invalidator {
	return &Invalidator{cache: c}
}

// AfterObjectMutation is used for EditX/MergeX-style mutations. Evict
// the (kind, owner, repo, id) row + every list_index row for
// (kind, owner, repo) since the mutation may have moved the resource
// between lists (e.g. an issue moving from `state=open` to `state=closed`).
func (i *Invalidator) AfterObjectMutation(ctx context.Context, kind, owner, repo, id string) {
	if i == nil || i.cache == nil {
		return
	}
	_ = i.cache.Invalidate(ctx, Key{Kind: kind, Owner: owner, Repo: repo, ID: id})
	_ = i.cache.InvalidateRepoLists(ctx, kind, owner, repo)
}

// AfterCreate is used for CreateX-style mutations. The new resource
// could appear in any list; flush them all for the (kind, owner, repo)
// triple.
func (i *Invalidator) AfterCreate(ctx context.Context, kind, owner, repo string) {
	if i == nil || i.cache == nil {
		return
	}
	_ = i.cache.InvalidateRepoLists(ctx, kind, owner, repo)
}

// AfterDelete is the same shape as AfterObjectMutation; aliased for
// readability at call sites where deletion is in progress.
func (i *Invalidator) AfterDelete(ctx context.Context, kind, owner, repo, id string) {
	i.AfterObjectMutation(ctx, kind, owner, repo, id)
}

// FlushRepo nukes every cached row (object + list) for (owner, repo).
// The fall-back used by mutating code paths whose impact is too broad
// to enumerate (e.g. DeleteRepo, but also the safe default when a new
// mutation method is added before its eviction policy is wired in).
func (i *Invalidator) FlushRepo(ctx context.Context, owner, repo string) {
	if i == nil || i.cache == nil {
		return
	}
	_ = i.cache.InvalidateRepoLists(ctx, "", owner, repo)
	// No bulk-delete-by-owner-repo for objects — the trade-off is that
	// FlushRepo is a soft flush. Callers who need true scorched-earth
	// invalidation should reach for Cache.Nuke instead.
}
