package forgejo

import (
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
)

// Default TTLs for #42's read cache. Configurable via the cache
// section of ~/.config/gaia/config.yaml; the values here are the
// fallbacks every provider method uses when no override is wired in.
//
// Single-resource reads have a 5-minute TTL bounded by
// ETag/If-Modified-Since: stale entries still trigger a conditional
// GET, so the TTL only governs how often we *check* freshness, not
// the maximum age of correct data.
//
// List reads have a tight 30-second TTL because forge endpoints
// rarely emit a useful ETag on list responses; the only correctness
// guarantee is "younger than TTL".
const (
	// CacheTTLSingle is the default TTL for single-resource reads
	// (GetIssue, GetPullRequest, etc.).
	CacheTTLSingle = 5 * time.Minute
	// CacheTTLList is the default TTL for list-style reads
	// (ListIssues, ListPullRequests, search).
	CacheTTLList = 30 * time.Second
)

// kindIssue, kindPR, kindRelease, ... are the cache "kind" labels
// every provider method uses when constructing a cache.Key. Keep
// these short and stable — they're part of the on-disk schema.
const (
	kindIssue    = "issue"
	kindPR       = "pr"
	kindRelease  = "release"
	kindWiki     = "wiki"
	kindPackage  = "package"
	kindWebhook  = "webhook"
	kindDelivery = "delivery"
)

// itoa is a tiny strconv.Itoa wrapper so call sites read uniformly:
// `itoa(issueNumber)` reads cleaner than `strconv.Itoa(...)`.
func itoa(n int) string { return strconv.Itoa(n) }

// itoa64 converts an int64 to its decimal string representation.
// Used for cache keys built from int64 IDs (webhook IDs, delivery IDs).
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// cacheKey is the single-line constructor every provider method uses
// when building a cache.Key. Centralised so a future kind-rename
// touches one site, not twenty.
func cacheKey(kind, owner, repo, id string) cache.Key {
	return cache.Key{Kind: kind, Owner: owner, Repo: repo, ID: id}
}
