// Package github implements core/provider.Provider for github.com
// (and GitHub Enterprise). The structure mirrors core/forgejo
// closely; the differences are confined to:
//
//   - Auth header: `Authorization: Bearer <token>` (works for both
//     classic and fine-grained PATs; the documented preference for
//     post-2022 GitHub auth).
//   - Standard headers: `Accept: application/vnd.github+json` and
//     `X-GitHub-Api-Version: 2022-11-28` per GitHub's API guidance.
//   - Diff fetching: requires `Accept: application/vnd.github.v3.diff`
//     to receive raw diff text (returns JSON otherwise).
//   - Search: lives at `/search/issues` (not the Forgejo
//     `/repos/issues/search`) and returns a paginated wrapper with
//     `total_count` + `items` rather than a bare array.
//
// Retry policy + error mapping are shared with Forgejo: 5xx-on-safe-
// verb retries once after backoff; 4xx never retries; status codes
// translate to gaia exit codes via core/exitcode.FromHTTP.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/version"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultRetryWait  = 500 * time.Millisecond
	maxErrorBodyBytes = 4096

	defaultAccept = "application/vnd.github+json"
	apiVersion    = "2022-11-28"
	apiVersionHdr = "X-GitHub-Api-Version"
	diffAccept    = "application/vnd.github.v3.diff"
	productionAPI = "https://api.github.com"
)

// Client is the HTTP-level GitHub API client. Safe for concurrent use.
type Client struct {
	baseURL    string
	token      string
	userAgent  string
	httpClient *http.Client
	retryWait  time.Duration
	// cache is the optional read cache (#42). When non-nil GetCached
	// consults it before each request; nil disables caching.
	//
	// Typed as the [cache.Cache] interface (not the concrete SQLite
	// type) so this package doesn't compile `modernc.org/sqlite` —
	// see #158. Production callers pass in a *sqlite.Store via
	// internal/forgebuilder; tests pass in a *cache.Memory.
	cache cache.Cache
}

// Options configure a Client. BaseURL defaults to api.github.com when
// empty; Token is required for authenticated calls.
//
// WikiRemoteFunc is a test hook: when non-nil, the wiki cache uses its
// return value as the clone/push URL instead of computing one from the
// production GitHub host. Production callers leave it nil; tests inject
// a `file://` path to a local bare repo so the suite stays offline.
type Options struct {
	BaseURL        string
	Token          string
	UserAgent      string
	HTTPClient     *http.Client
	RetryWait      time.Duration
	WikiRemoteFunc func(owner, repo string) string
	// Cache, when non-nil, enables the read cache (#42) for this
	// client. Leave nil to disable. Any [cache.Cache] implementation
	// works — production wires `core/cache/sqlite`, tests use
	// [cache.Memory].
	Cache cache.Cache
}

// New constructs a Client with sensible defaults.
func New(opts Options) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = productionAPI
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "gaia/" + version.Version
	}
	rw := opts.RetryWait
	if rw == 0 {
		rw = defaultRetryWait
	}
	return &Client{
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		token:      opts.Token,
		userAgent:  ua,
		httpClient: httpClient,
		retryWait:  rw,
		cache:      opts.Cache,
	}
}

// Get issues a GET and decodes the JSON response into out.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, defaultAccept, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(resp, http.MethodGet, path)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("decode %s %s", http.MethodGet, path))
	}
	return nil
}

// GetRaw issues a GET and returns the response body verbatim. Used
// for diff fetching where we explicitly request `text/diff` via
// Accept and want the raw bytes back.
func (c *Client) GetRaw(ctx context.Context, path, accept string) ([]byte, error) {
	if accept == "" {
		accept = "*/*"
	}
	resp, err := c.do(ctx, http.MethodGet, path, accept, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.statusError(resp, http.MethodGet, path)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Network, fmt.Sprintf("read %s %s", http.MethodGet, path))
	}
	return body, nil
}

// Post issues a POST with a JSON-encoded body.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.writeRequest(ctx, http.MethodPost, path, body, out)
}

// Patch issues a PATCH.
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.writeRequest(ctx, http.MethodPatch, path, body, out)
}

// Put issues a PUT with a JSON-encoded body. GitHub uses PUT for
// declarative-replace endpoints — notably branch protection
// (`/repos/{o}/{r}/branches/{branch}/protection`), where the request
// supplies the full desired state. Shares the same do() retry/error
// machinery as Post/Patch; PUT is treated as unsafe (no auto-retry).
func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.writeRequest(ctx, http.MethodPut, path, body, out)
}

// Delete issues a DELETE.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.writeRequest(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) writeRequest(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("marshal %s %s body", method, path))
		}
		reader = bytes.NewReader(raw)
	}
	resp, err := c.do(ctx, method, path, defaultAccept, reader)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(resp, method, path)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("decode %s %s", method, path))
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path, accept string, body io.Reader) (*http.Response, error) {
	urlStr := c.buildURL(path)
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("build %s %s", method, path))
	}
	c.setHeaders(req, accept, body != nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, exitcode.Wrap(scrubError(err, c.token), exitcode.Network, fmt.Sprintf("%s %s", method, path))
	}
	if !shouldRetry(method, resp.StatusCode) {
		return resp, nil
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	select {
	case <-time.After(c.retryWait):
	case <-ctx.Done():
		return nil, exitcode.Wrap(ctx.Err(), exitcode.Network, fmt.Sprintf("%s %s (canceled before retry)", method, path))
	}

	req2, err := http.NewRequestWithContext(ctx, method, urlStr, nil)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("rebuild %s %s for retry", method, path))
	}
	c.setHeaders(req2, accept, false)
	resp, err = c.httpClient.Do(req2)
	if err != nil {
		return nil, exitcode.Wrap(scrubError(err, c.token), exitcode.Network, fmt.Sprintf("%s %s (retry)", method, path))
	}
	return resp, nil
}

func (c *Client) setHeaders(req *http.Request, accept string, hasBody bool) {
	if c.token != "" {
		// Bearer is the post-2022 GitHub-recommended scheme; works for
		// both classic and fine-grained PATs.
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", accept)
	req.Header.Set(apiVersionHdr, apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
}

func (c *Client) buildURL(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.baseURL + path
}

func (c *Client) statusError(resp *http.Response, method, path string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	snippet := strings.TrimSpace(string(body))
	if snippet == "" {
		snippet = http.StatusText(resp.StatusCode)
	}
	return exitcode.Wrap(
		fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet),
		exitcode.FromHTTP(resp.StatusCode),
		fmt.Sprintf("%s %s", method, path),
	)
}

func shouldRetry(method string, status int) bool {
	if status < 500 || status >= 600 {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func scrubError(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, token) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(msg, token, "<redacted>"))
}
