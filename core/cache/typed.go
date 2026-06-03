package cache

import (
	"context"
	"encoding/json"
	"time"
)

// Typed is a generic, type-safe cache-aside view over a [Cache]. It
// hides the byte-level [Entry]/[Key] plumbing and the lookup → miss →
// fetch → store dance behind a single [Typed.GetOr] call that speaks in
// a Go type T, JSON-marshalling values on the way in and unmarshalling
// them on the way out.
//
// The wide [Cache] interface has ~10 methods; a consumer that only
// wants "give me T, computing it on a miss" should not have to think
// about LookupList, StoreList, Scan, or Nuke. Typed is the narrow slice
// that makes the interface cheap to adopt outside the HTTP clients (the
// motivation for #314 / ADR 0001).
//
// Construct one as a struct literal:
//
//	gitRemotes := cache.Typed[*ParsedRemote]{Cache: c, Kind: "git-remote", TTL: 24 * time.Hour}
//	remote, err := gitRemotes.GetOr(ctx, "", "", path, func(ctx context.Context) (*ParsedRemote, error) {
//	    return parseGitRemote(path)
//	})
//
// Kind partitions the underlying (kind, owner, repo, id) key space, so
// two Typed views with different Kinds over the same [Cache] never
// collide even when they share an id. TTL is applied to every stored
// value.
//
// A nil Cache is tolerated the same way the rest of the package
// tolerates it: GetOr always calls fetch and stores nothing, Invalidate
// and InvalidateList are no-ops. This lets callers wire a Typed
// unconditionally and let the "caching off" case fall through
// harmlessly.
type Typed[T any] struct {
	Cache Cache
	Kind  string
	TTL   time.Duration
}

// typedOwner is the placeholder Owner slot used when a caller passes an
// empty owner (owner-less resources, e.g. a path-keyed git-remote
// parse). The underlying [Cache.Store] contract requires a non-empty
// owner; substituting a fixed sentinel keeps owner-less rows in their
// own corner of the (kind, owner, repo, id) space without ever aliasing
// a real forge owner.
const typedOwner = "_typed"

// key builds the underlying object [Key] for a (owner, repo, id) tuple,
// substituting the sentinel owner when owner is empty.
func (t Typed[T]) key(owner, repo, id string) Key {
	if owner == "" {
		owner = typedOwner
	}
	return Key{Kind: t.Kind, Owner: owner, Repo: repo, ID: id}
}

// GetOr returns the value cached under (owner, repo, id), decoded into
// T. On a miss — no live entry, an expired/stale entry, or a stored
// payload that no longer decodes into T — it calls fetch exactly once,
// stores the result with the Typed's TTL, and returns it. A live cache
// hit never calls fetch.
//
// fetch's error is returned verbatim and nothing is stored, so a failed
// upstream call never pollutes the cache. The cache layer itself never
// produces an error from GetOr: lookup errors, decode errors, and store
// errors all degrade to "treat as a miss" / "value is still valid" so a
// flaky cache can't break a call whose fetch succeeded.
func (t Typed[T]) GetOr(ctx context.Context, owner, repo, id string, fetch func(context.Context) (T, error)) (T, error) {
	k := t.key(owner, repo, id)

	if t.Cache != nil {
		if entry, ok, err := t.Cache.Lookup(ctx, k); err == nil && ok && !entry.Stale {
			var v T
			if json.Unmarshal(entry.Payload, &v) == nil {
				return v, nil
			}
			// Corrupt / wrong-shape payload: fall through to fetch and
			// overwrite it, rather than surfacing a decode error for
			// something the caller can recompute.
		}
	}

	v, err := fetch(ctx)
	if err != nil {
		var zero T
		return zero, err
	}

	if t.Cache != nil {
		if payload, merr := json.Marshal(v); merr == nil {
			// Best-effort store: a write failure (incl. the empty-id
			// case the underlying contract rejects) leaves the freshly
			// fetched value untouched.
			_ = t.Cache.Store(ctx, Entry{Key: k, FetchedAt: time.Now(), TTL: t.TTL, Payload: payload})
		}
	}
	return v, nil
}

// Invalidate removes the value cached under (owner, repo, id). A missing
// key is a no-op, matching [Cache.Invalidate].
func (t Typed[T]) Invalidate(ctx context.Context, owner, repo, id string) error {
	if t.Cache == nil {
		return nil
	}
	return t.Cache.Invalidate(ctx, t.key(owner, repo, id))
}

// InvalidateList clears every cached list query for this Typed's Kind
// scoped to (owner, repo), mapping to [Cache.InvalidateRepoLists]. It is
// the companion to [Typed.Invalidate] for callers that also cache list
// responses under the same Kind. A nil Cache is a no-op.
func (t Typed[T]) InvalidateList(ctx context.Context, owner, repo string) error {
	if t.Cache == nil {
		return nil
	}
	if owner == "" {
		owner = typedOwner
	}
	return t.Cache.InvalidateRepoLists(ctx, t.Kind, owner, repo)
}
