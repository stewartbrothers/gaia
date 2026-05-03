package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiPackage mirrors the subset of Forgejo's package record we read.
// Owner is sometimes returned as a nested user/org object and
// sometimes (older Gitea releases) as a bare string; we only decode
// the structured form here — the bare-string variant predates
// Forgejo 1.20 and isn't a target.
//
// Size lives under the per-version metadata in some registries
// (Forgejo's container records expose it directly) but isn't always
// present on the list payload — leaving it omitempty keeps the
// trimmed view honest about that.
type apiPackage struct {
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Owner     apiUser   `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size,omitempty"`
}

func (a *apiPackage) toType() types.Package {
	return types.Package{
		Type:      a.Type,
		Name:      a.Name,
		Version:   a.Version,
		Owner:     a.Owner.Login,
		CreatedAt: a.CreatedAt,
		Size:      a.Size,
	}
}

// ListPackages returns packages owned by `owner`. Forgejo's endpoint
// is `/packages/{owner}` — packages are NOT scoped to a repo, so the
// CLI/MCP surface uses --owner instead of --repo.
func (p *Provider) ListPackages(ctx context.Context, owner string, opts provider.ListPackagesOptions) ([]types.Package, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.Type != "" {
		q.Set("type", opts.Type)
	}
	if opts.Q != "" {
		q.Set("q", opts.Q)
	}

	path := fmt.Sprintf("/packages/%s?%s", url.PathEscape(owner), q.Encode())
	var raw []apiPackage
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Package, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetPackage fetches one package version by (owner, type, name,
// version). All four are required and form the URL path.
func (p *Provider) GetPackage(ctx context.Context, owner, pkgType, name, version string) (*types.Package, error) {
	path := packagePath(owner, pkgType, name, version)
	var raw apiPackage
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeletePackage removes one package version. 204 is the documented
// success status; the client treats any 2xx as success.
func (p *Provider) DeletePackage(ctx context.Context, owner, pkgType, name, version string) error {
	return p.client.Delete(ctx, packagePath(owner, pkgType, name, version))
}

// packagePath builds `/packages/{owner}/{type}/{name}/{version}` with
// each segment URL-escaped. Forgejo allows package names to include
// characters like "@" and "/" (notably for npm scoped packages),
// which would otherwise corrupt the path.
func packagePath(owner, pkgType, name, version string) string {
	return fmt.Sprintf("/packages/%s/%s/%s/%s",
		url.PathEscape(owner),
		url.PathEscape(pkgType),
		url.PathEscape(name),
		url.PathEscape(version),
	)
}
