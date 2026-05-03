package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

type apiRelease struct {
	ID              int64      `json:"id"`
	TagName         string     `json:"tag_name"`
	Name            string     `json:"name"`
	Body            string     `json:"body"`
	Draft           bool       `json:"draft"`
	Prerelease      bool       `json:"prerelease"`
	Author          apiUser    `json:"author"`
	TargetCommitish string     `json:"target_commitish"`
	CreatedAt       time.Time  `json:"created_at"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

func (a *apiRelease) toType() types.Release {
	return types.Release{
		ID:              a.ID,
		TagName:         a.TagName,
		Name:            a.Name,
		Body:            a.Body,
		Draft:           a.Draft,
		Prerelease:      a.Prerelease,
		Author:          types.User{Login: a.Author.Login},
		TargetCommitish: a.TargetCommitish,
		CreatedAt:       a.CreatedAt,
		PublishedAt:     a.PublishedAt,
	}
}

// ListReleases returns releases newest-first.
func (p *Provider) ListReleases(ctx context.Context, owner, repo string, opts provider.ListReleasesOptions) ([]types.Release, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/releases?%s", owner, repo, q.Encode())
	var raw []apiRelease
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Release, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetRelease fetches one release by tag.
func (p *Provider) GetRelease(ctx context.Context, owner, repo, tag string) (*types.Release, error) {
	var raw apiRelease
	path := fmt.Sprintf("/repos/%s/%s/releases/tags/%s", owner, repo, url.PathEscape(tag))
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// CreateRelease creates a new release.
func (p *Provider) CreateRelease(ctx context.Context, owner, repo string, opts provider.CreateReleaseOptions) (*types.Release, error) {
	var raw apiRelease
	if err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/releases", owner, repo), opts, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterCreate(ctx, kindRelease, owner, repo)
	out := raw.toType()
	return &out, nil
}

// EditRelease patches a release identified by tag (looks up ID first).
func (p *Provider) EditRelease(ctx context.Context, owner, repo, tag string, opts provider.EditReleaseOptions) (*types.Release, error) {
	current, err := p.GetRelease(ctx, owner, repo, tag)
	if err != nil {
		return nil, err
	}
	var raw apiRelease
	path := fmt.Sprintf("/repos/%s/%s/releases/%d", owner, repo, current.ID)
	if err := p.client.Patch(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindRelease, owner, repo, tag)
	out := raw.toType()
	return &out, nil
}

// DeleteRelease removes a release by tag (looks up ID first).
func (p *Provider) DeleteRelease(ctx context.Context, owner, repo, tag string) error {
	current, err := p.GetRelease(ctx, owner, repo, tag)
	if err != nil {
		return err
	}
	if err := p.client.Delete(ctx, fmt.Sprintf("/repos/%s/%s/releases/%d", owner, repo, current.ID)); err != nil {
		return err
	}
	cache.NewInvalidator(p.client.cache).AfterDelete(ctx, kindRelease, owner, repo, tag)
	return nil
}

// UploadReleaseAsset attaches a file to an existing release. GitHub
// uses a separate host (uploads.github.com) for asset uploads, with
// the body sent as the request body directly (NOT multipart) and the
// Content-Type set to the asset's actual MIME type.
//
// The base URL for upload typically derives from `api.github.com` →
// `uploads.github.com`. We honour the same base substitution when a
// custom api host is configured (e.g., GHES at api.example.com →
// uploads.example.com), with the same /api/v3 prefix when present.
//
// Bypasses the JSON Post path because the body is binary.
func (p *Provider) UploadReleaseAsset(ctx context.Context, owner, repo string, releaseID int64, name, contentType string, body io.Reader) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	uploadBase := uploadHostFor(p.client.baseURL)
	q := url.Values{"name": []string{name}}
	uploadURL := fmt.Sprintf("%s/repos/%s/%s/releases/%d/assets?%s",
		uploadBase, owner, repo, releaseID, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
	if err != nil {
		return exitcode.Wrap(err, exitcode.Generic, "build asset upload request")
	}
	if p.client.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.client.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", p.client.userAgent)

	resp, err := p.client.httpClient.Do(req)
	if err != nil {
		return exitcode.Wrap(err, exitcode.Network, fmt.Sprintf("POST %s", uploadURL))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return exitcode.Wrap(
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(snippet)),
			exitcode.FromHTTP(resp.StatusCode),
			fmt.Sprintf("upload asset %q to release %d", name, releaseID),
		)
	}
	return nil
}

// uploadHostFor maps an api host to its upload host. GitHub.com:
// api.github.com → uploads.github.com. Enterprise (GHES):
// api.<host>/api/v3 → uploads.<host>/api/uploads. Falls back to the
// caller's base URL on no match (the upload will 404 — surfacing the
// misconfiguration loudly).
func uploadHostFor(apiBase string) string {
	if strings.HasPrefix(apiBase, "https://api.github.com") {
		return "https://uploads.github.com"
	}
	// GHES style: https://api.example.com/api/v3 → https://uploads.example.com/api/uploads
	if strings.Contains(apiBase, "/api/v3") {
		return strings.Replace(apiBase, "/api/v3", "/api/uploads", 1)
	}
	return apiBase
}
