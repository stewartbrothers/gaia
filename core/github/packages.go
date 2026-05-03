// Package github: GitHub Packages support. GitHub keys packages by
// (owner, package_type, name, version_id) — version_id is a numeric ID
// per version, NOT the human-readable tag/semver. The provider hides
// that mismatch: callers pass a `version` string; if it parses as an
// integer, we use it as the version_id directly; otherwise we list
// versions and resolve by either the version's `name` field
// (npm/maven semver, container manifest digest) or the
// `metadata.container.tags[]` (container tags like "latest", "v1.2.3").
//
// User-vs-org dispatch: GitHub has separate /users/{o}/packages and
// /orgs/{o}/packages endpoints. We probe `GET /users/{owner}` once to
// read the account `type` ("User" or "Organization") and route every
// subsequent call through the matching prefix. The probe is cached in
// the Provider value for the duration of the operation only — no
// cross-call cache, since owner kind is essentially static and the
// extra round-trip is one HTTP call we'd otherwise need anyway.
//
// UploadPackage stays a NotImplemented stub here; PR 2 (#122) replaces
// it with a real implementation across providers.
package github

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiPackageListEntry is one row of GET /users/{o}/packages or
// /orgs/{o}/packages. GitHub's list endpoint surfaces only package-
// family fields (no per-version data), so the trimmed types.Package
// gets Version="" on list — callers wanting per-version detail call
// GetPackage with a specific version reference.
type apiPackageListEntry struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	PackageType string    `json:"package_type"`
	Owner       apiUser   `json:"owner"`
	CreatedAt   time.Time `json:"created_at"`
}

func (a *apiPackageListEntry) toType() types.Package {
	return types.Package{
		Type:      a.PackageType,
		Name:      a.Name,
		Owner:     a.Owner.Login,
		CreatedAt: a.CreatedAt,
	}
}

// apiPackageVersion mirrors GET /users/{o}/packages/{type}/{name}/versions/{vid}.
// Name is the version's wire identifier — for container packages this
// is the manifest digest (sha256:...); for npm/maven it's the semver
// string. tags[] under metadata.container exposes human-friendly
// container tags ("latest", "v1.2.3").
type apiPackageVersion struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Metadata  struct {
		PackageType string `json:"package_type,omitempty"`
		Container   struct {
			Tags []string `json:"tags,omitempty"`
		} `json:"container"`
	} `json:"metadata"`
}

// apiOwnerInfo is the GET /users/{owner} subset we read for type dispatch.
// The same endpoint serves both users and organizations; the `type`
// field discriminates ("User" or "Organization").
type apiOwnerInfo struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// ownerScope returns "users" or "orgs" for the given owner. Single
// extra round-trip; the result drives the path prefix for every
// subsequent packages call. Defaults to "users" on any error so the
// caller still gets a meaningful HTTP error from the actual packages
// call rather than a vague meta failure.
func (p *Provider) ownerScope(ctx context.Context, owner string) (string, error) {
	var info apiOwnerInfo
	if err := p.client.Get(ctx, fmt.Sprintf("/users/%s", url.PathEscape(owner)), &info); err != nil {
		return "", err
	}
	if info.Type == "Organization" {
		return "orgs", nil
	}
	return "users", nil
}

// ListPackages lists packages owned by `owner`, dispatching to
// /users/{o}/packages or /orgs/{o}/packages based on owner type.
// Pagination uses GitHub's standard `page` + `per_page`.
func (p *Provider) ListPackages(ctx context.Context, owner string, opts provider.ListPackagesOptions) ([]types.Package, *provider.Page, error) {
	scope, err := p.ownerScope(ctx, owner)
	if err != nil {
		return nil, nil, err
	}

	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.Type != "" {
		q.Set("package_type", opts.Type)
	}
	// GitHub's list endpoint has no name-substring filter; opts.Q is
	// dropped silently (same shape as the documented Forgejo→GitHub
	// gaps in provider-parity.md).

	path := fmt.Sprintf("/%s/%s/packages?%s", scope, url.PathEscape(owner), q.Encode())
	var raw []apiPackageListEntry
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Package, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetPackage fetches one package version. The `version` parameter is
// resolved as follows: if it parses as a non-negative integer, treat
// it as the GitHub version_id and fetch directly. Otherwise list
// versions and match against either the version's `name` field or
// container tags; the first match wins.
func (p *Provider) GetPackage(ctx context.Context, owner, pkgType, name, version string) (*types.Package, error) {
	scope, err := p.ownerScope(ctx, owner)
	if err != nil {
		return nil, err
	}

	versionID, err := p.resolveVersionID(ctx, scope, owner, pkgType, name, version)
	if err != nil {
		return nil, err
	}

	var raw apiPackageVersion
	path := packageVersionPath(scope, owner, pkgType, name, versionID)
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	return &types.Package{
		Type:      pkgType,
		Name:      name,
		Version:   raw.Name,
		Owner:     owner,
		CreatedAt: raw.CreatedAt,
	}, nil
}

// DeletePackage removes one package version, after resolving the
// version reference to a numeric ID by the same rules as GetPackage.
// Returns nil on 204.
func (p *Provider) DeletePackage(ctx context.Context, owner, pkgType, name, version string) error {
	scope, err := p.ownerScope(ctx, owner)
	if err != nil {
		return err
	}
	versionID, err := p.resolveVersionID(ctx, scope, owner, pkgType, name, version)
	if err != nil {
		return err
	}
	return p.client.Delete(ctx, packageVersionPath(scope, owner, pkgType, name, versionID))
}

// resolveVersionID takes a caller-supplied `version` string and turns
// it into a numeric GitHub version_id. Pure-integer inputs pass
// through; everything else triggers a list-then-match against the
// version's `name` and (for containers) `metadata.container.tags`.
func (p *Provider) resolveVersionID(ctx context.Context, scope, owner, pkgType, name, version string) (int64, error) {
	if id, err := strconv.ParseInt(version, 10, 64); err == nil && id >= 0 {
		return id, nil
	}
	versions, err := p.listVersions(ctx, scope, owner, pkgType, name)
	if err != nil {
		return 0, err
	}
	for _, v := range versions {
		if v.Name == version {
			return v.ID, nil
		}
		for _, tag := range v.Metadata.Container.Tags {
			if tag == version {
				return v.ID, nil
			}
		}
	}
	return 0, exitcode.Errorf(exitcode.NotFound,
		"no version %q found for %s/%s/%s under owner %q", version, scope, pkgType, name, owner)
}

// listVersions fetches ALL versions for a package. GitHub paginates
// via `?per_page=` (max 100); for the version-name-resolution path we
// read just the first page — packages typically have far fewer than
// 100 versions, and the alternative (paging until exhausted) doubles
// latency on the common case for a small win on the long tail. If a
// caller hits the long-tail case, they can pass the numeric version
// ID directly and skip this code path.
func (p *Provider) listVersions(ctx context.Context, scope, owner, pkgType, name string) ([]apiPackageVersion, error) {
	q := url.Values{}
	q.Set("per_page", "100")
	path := fmt.Sprintf("/%s/%s/packages/%s/%s/versions?%s",
		scope,
		url.PathEscape(owner),
		url.PathEscape(pkgType),
		url.PathEscape(name),
		q.Encode(),
	)
	var raw []apiPackageVersion
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// packageVersionPath builds the per-version URL used by GET and DELETE.
func packageVersionPath(scope, owner, pkgType, name string, versionID int64) string {
	return fmt.Sprintf("/%s/%s/packages/%s/%s/versions/%d",
		scope,
		url.PathEscape(owner),
		url.PathEscape(pkgType),
		url.PathEscape(name),
		versionID,
	)
}

// UploadPackage is intentionally NotImplemented on GitHub. GitHub
// Packages publish flows are per-registry: npm uses `npm publish`
// against `npm.pkg.github.com` (semver versioning + tarball upload +
// per-version metadata); container/GHCR uses Docker registry v2 push
// (manifest + layer blobs); maven/nuget/rubygems each have their own
// publish protocols. Folding all of those into one provider method
// isn't useful — the per-registry semantics differ enough that a
// single shape would force callers to pre-format an
// already-protocol-specific payload.
//
// The follow-up issue tracks per-kind dispatch (e.g.,
// `UploadPackage(pkgType="container", ...)` proxying through GHCR's
// v2 API). For now, the documented error tells callers exactly what
// to look up.
//
// See `docs/provider-parity.md` row for `UploadPackage`.
func (p *Provider) UploadPackage(_ context.Context, _, pkgType, _, _ string, _ provider.UploadPackageOptions, _ io.Reader) error {
	return exitcode.Errorf(exitcode.Generic,
		"GitHub Packages upload is not implemented for pkgType=%q — per-registry publish flows (npm publish, GHCR v2 push, ...) are tracked in a #122 follow-up", pkgType)
}
