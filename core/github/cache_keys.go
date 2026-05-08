package github

import (
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
)

// Default TTLs for #42's read cache; mirrors core/forgejo/cache_keys.go.
const (
	CacheTTLSingle = 5 * time.Minute
	CacheTTLList   = 30 * time.Second
)

const (
	kindIssue    = "issue"
	kindPR       = "pr"
	kindRelease  = "release"
	kindWiki     = "wiki"
	kindPackage  = "package"
	kindWebhook  = "webhook"
	kindDelivery = "delivery"
)

func itoa(n int) string { return strconv.Itoa(n) }

// itoa64 converts an int64 to its decimal string representation.
// Used for cache keys built from int64 IDs (webhook IDs, delivery IDs).
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// cacheKey is the single-line constructor every provider method uses
// when building a cache.Key.
func cacheKey(kind, owner, repo, id string) cache.Key {
	return cache.Key{Kind: kind, Owner: owner, Repo: repo, ID: id}
}
