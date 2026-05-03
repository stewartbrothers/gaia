package github

import (
	"context"
	"sync"
)

// Provider implements core/provider.Provider for GitHub. Methods are
// added in phases; commit 1 of the Phase 2 stack lands the foundation
// (Whoami) and the HTTP client. Subsequent commits add issues
// (#32), PRs (#33), diff (#34), and comments (#35).
//
// Each method delegates to Client and reuses the trim-at-boundary
// pattern from core/forgejo: an internal apiX shape decodes only the
// fields we need, then a toType() converter produces the trimmed
// core/types value.
//
// Wikis are the exception: GitHub doesn't expose a REST surface for
// them, so the Provider also carries a wikiCache (a per-process
// working clone manager). The cache is constructed lazily on first
// wiki call and reused thereafter for the lifetime of the Provider.
type Provider struct {
	client         *Client
	token          string
	wikiRemoteFunc func(owner, repo string) string

	wikiCacheOnce sync.Once
	wikiCacheVal  *wikiCache
	wikiCacheErr  error
}

// NewProvider builds a Provider over a freshly-constructed Client.
func NewProvider(opts Options) *Provider {
	return &Provider{
		client:         New(opts),
		token:          opts.Token,
		wikiRemoteFunc: opts.WikiRemoteFunc,
	}
}

// wikicache returns the lazily-constructed wiki cache for this
// Provider, building it on first call. Errors building it (e.g.
// failure to mkdir the cache root) are surfaced to every wiki
// operation so callers see a consistent diagnostic.
func (p *Provider) wikicache() (*wikiCache, error) {
	p.wikiCacheOnce.Do(func() {
		p.wikiCacheVal, p.wikiCacheErr = newWikiCache(p.token)
	})
	return p.wikiCacheVal, p.wikiCacheErr
}

// wikiRemote computes the clone/push URL for {owner}/{repo}'s wiki.
// In production this is the canonical github.com URL with the PAT
// embedded; tests inject a `file://` URL via Options.WikiRemoteFunc
// to keep the suite offline.
func (p *Provider) wikiRemote(owner, repo string) string {
	if p.wikiRemoteFunc != nil {
		return p.wikiRemoteFunc(owner, repo)
	}
	return wikiRemoteURL("", owner, repo, p.token)
}

// apiCurrentUser is the shape of GET /user that we read.
type apiCurrentUser struct {
	Login string `json:"login"`
}

// Whoami returns the authenticated user's login. Foundational for the
// `gaia auth gh` validation flow and for the eventual `gaia whoami`
// against github.com.
func (p *Provider) Whoami(ctx context.Context) (string, error) {
	var u apiCurrentUser
	if err := p.client.Get(ctx, "/user", &u); err != nil {
		return "", err
	}
	return u.Login, nil
}
