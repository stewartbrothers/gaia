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
	kindIssue   = "issue"
	kindPR      = "pr"
	kindRelease = "release"
	kindWiki    = "wiki"
	kindPackage = "package"
	kindWebhook = "webhook"
)

func itoa(n int) string { return strconv.Itoa(n) }

// cacheKey is the single-line constructor every provider method uses
// when building a cache.Key.
func cacheKey(kind, owner, repo, id string) cache.Key {
	return cache.Key{Kind: kind, Owner: owner, Repo: repo, ID: id}
}
