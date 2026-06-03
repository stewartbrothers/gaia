package autodetect

import (
	"context"
	"path/filepath"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
)

// gitRemoteKind is the cache Kind under which parsed git remotes live.
// gitRemoteTTL bounds how long a parsed remote is trusted before
// re-shelling out to git — a day is generous since a checkout's origin
// URL effectively never changes mid-session.
const (
	gitRemoteKind = "git-remote"
	gitRemoteTTL  = 24 * time.Hour
)

// FromGitRemoteCached is the cache-aware companion to [FromGitRemote].
// It returns the parsed remote for (dir, name), shelling out to git only
// on a cold cache and caching the result keyed by the directory's
// absolute path (and remote name) for [gitRemoteTTL].
//
// It is the first non-HTTP-client adopter of [cache.Typed] (#314): the
// lookup → miss → fetch → store dance is entirely handled by
// [cache.Typed.GetOr], so this wrapper stays a few lines. A nil cache
// degrades to a plain [FromGitRemote] call — identical behaviour, no
// caching — so callers that have caching disabled wire it unconditionally.
func FromGitRemoteCached(ctx context.Context, c cache.Cache, dir, name string) (*Repo, error) {
	if name == "" {
		name = "origin"
	}
	// Key by the absolute path so two different checkouts never alias,
	// even if invoked with different relative spellings of the same dir.
	id := dir
	if abs, err := filepath.Abs(dir); err == nil {
		id = abs
	}

	remotes := cache.Typed[*Repo]{Cache: c, Kind: gitRemoteKind, TTL: gitRemoteTTL}
	return remotes.GetOr(ctx, "", name, id, func(context.Context) (*Repo, error) {
		return FromGitRemote(dir, name)
	})
}
