// Package forgejo implements core.Provider for Forgejo (and
// Gitea-compatible) forges. This file is the HTTP foundation: the
// Client type, request execution, retry policy, and error mapping.
// Provider methods (ListIssues, GetPullRequest, ...) build on top of
// it in subsequent issues (#15..#19).
//
// Authentication uses the `Authorization: token <token>` header;
// errors are mapped to gaia exit codes via core/exitcode.FromHTTP.
// Retries: GET requests that hit 5xx are retried exactly once after a
// configurable backoff; non-idempotent verbs are never retried.
package forgejo

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

// Default values used when Options leaves a field zero.
const (
	defaultTimeout    = 30 * time.Second
	defaultRetryWait  = 500 * time.Millisecond
	maxErrorBodyBytes = 4096
)

// Client is the HTTP-level Forgejo API client. It is safe for
// concurrent use by multiple goroutines.
type Client struct {
	baseURL    string
	token      string
	userAgent  string
	httpClient *http.Client
	retryWait  time.Duration
	// cache is the optional read cache. When non-nil, GetCached and
	// (in future) GetListCached consult it before issuing upstream
	// requests; on a fresh hit the upstream call is skipped entirely.
	// nil disables caching for this client (used in tests, or when
	// the global `cache.enabled: false` config knob is set).
	//
	// Typed as the [cache.Cache] interface (not the concrete SQLite
	// type) so this package doesn't compile `modernc.org/sqlite` —
	// see #158. Production callers pass in a *sqlite.Store via
	// internal/forgebuilder; tests pass in a *cache.Memory.
	cache cache.Cache
}

// Options configure a Client. BaseURL and Token are required; other
// fields fall back to documented defaults.
type Options struct {
	// BaseURL is the API root, e.g. "https://your-forge.example.com/api/v1".
	BaseURL string
	// Token is the Forgejo personal access token. Sent as
	// `Authorization: token <Token>`.
	Token string
	// UserAgent is the User-Agent header value. Defaults to
	// "gaia/<version>".
	UserAgent string
	// HTTPClient overrides the underlying *http.Client; defaults to
	// one with a 30-second timeout.
	HTTPClient *http.Client
	// RetryWait is the backoff between the first failed attempt and
	// the single retry on 5xx. Defaults to 500ms.
	RetryWait time.Duration
	// Cache, when non-nil, plugs the read cache into GetCached calls.
	// Leave nil to disable caching for this client. Any [cache.Cache]
	// implementation works — production wires `core/cache/sqlite`,
	// tests use [cache.Memory].
	Cache cache.Cache
}

// New constructs a Client. Trailing slashes on BaseURL are normalized.
func New(opts Options) *Client {
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

// Get issues a GET to path (relative to BaseURL) and decodes a JSON
// response into out. Pass nil for out to discard the body. Non-2xx
// responses are returned as exitcode.Error values carrying the
// mapped exit code.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, "application/json", nil)
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

// Post issues a POST with a JSON-encoded body and decodes the JSON
// response into out. Body may be nil; out may be nil. Errors map the
// same way Get's do — non-2xx → exitcode.FromHTTP wrapped error.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.writeRequest(ctx, http.MethodPost, path, body, out)
}

// Patch issues a PATCH with a JSON-encoded body and decodes JSON into out.
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.writeRequest(ctx, http.MethodPatch, path, body, out)
}

// Delete issues a DELETE. The response body is discarded; only the
// status drives success/error. 204 and 200 are both treated as success.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.writeRequest(ctx, http.MethodDelete, path, nil, nil)
}

// writeRequest is the shared implementation for Post/Patch/Delete.
// Marshals body to JSON when non-nil; sends Accept and Content-Type
// JSON; decodes into out when non-nil.
func (c *Client) writeRequest(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("marshal %s %s body", method, path))
		}
		reader = bytes.NewReader(raw)
	}
	resp, err := c.do(ctx, method, path, "application/json", reader)
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

// PostMultipart issues a POST with a multipart/form-data body. Used
// for the release-asset upload endpoint, which Forgejo's API requires
// be a multipart form with field name "attachment". The caller passes
// a populated bytes.Buffer + the matching Content-Type from
// multipart.Writer.FormDataContentType().
func (c *Client) PostMultipart(ctx context.Context, path, contentType string, body io.Reader, out any) error {
	urlStr := c.buildURL(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, body)
	if err != nil {
		return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("build POST %s", path))
	}
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return exitcode.Wrap(scrubError(err, c.token), exitcode.Network, fmt.Sprintf("POST %s", path))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(resp, http.MethodPost, path)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("decode POST %s", path))
	}
	return nil
}

// PutBinary issues a PUT with the body streamed as the request body
// (no multipart, no JSON wrapper). Used by Forgejo's generic-package
// upload endpoint, which expects the artifact bytes directly. Returns
// nil on any 2xx status (Forgejo returns 201 Created on first upload
// and 409 Conflict if the file already exists at that
// owner/name/version/file path). Streams `body` so large artifacts
// don't load into memory.
func (c *Client) PutBinary(ctx context.Context, path, contentType string, body io.Reader) error {
	urlStr := c.buildURL(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, urlStr, body)
	if err != nil {
		return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("build PUT %s", path))
	}
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return exitcode.Wrap(scrubError(err, c.token), exitcode.Network, fmt.Sprintf("PUT %s", path))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(resp, http.MethodPut, path)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// GetRaw issues a GET and returns the response body as bytes. Used
// for endpoints that return non-JSON payloads (notably `.diff`). The
// Accept header is set to `*/*`.
func (c *Client) GetRaw(ctx context.Context, path string) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, path, "*/*", nil)
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

	// Drain + close before retrying so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	select {
	case <-time.After(c.retryWait):
	case <-ctx.Done():
		return nil, exitcode.Wrap(ctx.Err(), exitcode.Network, fmt.Sprintf("%s %s (canceled before retry)", method, path))
	}

	// Re-issue. GET has no body so this is safe.
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
		req.Header.Set("Authorization", "token "+c.token)
	}
	req.Header.Set("Accept", accept)
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

// shouldRetry reports whether a (method, status) pair is retryable.
// 5xx is retried for safe (idempotent, body-less) verbs only.
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

// scrubError defensively strips the token out of a transport-level
// error string. Net/http errors normally do not echo the request
// header, but URL-shaped credentials or proxy errors occasionally do;
// belt and braces.
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
