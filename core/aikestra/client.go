// Package aikestra implements core/provider.Provider for Aikestra, an
// agent-first forge (public REST API v1, documented by the instance at
// GET <origin>/api/v1/openapi.json). This file is the HTTP foundation:
// the Client type, request execution, retry policy, cursor paging, and
// the mapping from Aikestra's error codes onto gaia exit codes.
//
// Aikestra's model is not a Forgejo/GitHub clone, so the mapping is not
// one-to-one:
//
//   - gaia's `owner` is the Aikestra Project slug; `repo` is a Repo name
//     within it. There is nothing above a Project.
//   - Issues belong to the Project, not to a Repo. Every issue method
//     addresses /projects/{owner}/issues and ignores `repo`.
//   - There are no labels. An Issue has a single `type` (feature, bug,
//     …); a requested label maps onto `type`.
//   - People are identified by email.
//
// Authentication is `Authorization: Bearer <token>` (a User access token
// or a Delegated Agent credential). Errors are always
// `{"error":{"code","message"}}`; see [mapError].
package aikestra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/version"
)

// Default values used when Options leaves a field zero.
const (
	defaultTimeout    = 30 * time.Second
	defaultRetryWait  = 500 * time.Millisecond
	maxErrorBodyBytes = 4096

	// defaultLimit and maxLimit mirror the server's paging bounds: limit
	// is 1–100, default 30. gaia's --limit is clamped to maxLimit rather
	// than sent and rejected.
	defaultLimit = 30
	maxLimit     = 100

	// maxWalkPages bounds a client-side walk over a cursor list (used
	// where gaia's contract has no page, e.g. comments). Aikestra has no
	// rate limiting yet, so don't let a runaway list hammer it.
	maxWalkPages = 50
)

// Client is the HTTP-level Aikestra API client. It is safe for
// concurrent use by multiple goroutines.
type Client struct {
	baseURL    string
	token      string
	userAgent  string
	httpClient *http.Client
	retryWait  time.Duration
}

// Options configure a Client. BaseURL and Token are required; other
// fields fall back to documented defaults.
type Options struct {
	// BaseURL is the API root, e.g. "https://aikestra.example/api/v1".
	BaseURL string
	// Token is the access token, sent as `Authorization: Bearer <Token>`.
	Token string
	// UserAgent defaults to "gaia/<version>".
	UserAgent string
	// HTTPClient overrides the underlying *http.Client; defaults to one
	// with a 30-second timeout.
	HTTPClient *http.Client
	// RetryWait is the backoff before the single retry of a GET that hit
	// a 5xx. Defaults to 500ms.
	RetryWait time.Duration
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
	}
}

// listResponse is the envelope every Aikestra list endpoint returns.
// NextCursor is nil only when nothing follows — including when the last
// page is exactly full — so it, not the page size, marks the end.
// Truncated is set by repo-content lists when the server stopped at a
// hard limit.
type listResponse[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Truncated  bool    `json:"truncated"`
}

// errorResponse is Aikestra's one error shape.
type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Get issues a GET to path with query and decodes the JSON response into
// out. Pass nil for out to discard the body.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// Post sends body as JSON and decodes the response into out.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

// Patch sends body as JSON and decodes the response into out.
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPatch, path, nil, body, out)
}

// Put sends body as JSON and decodes the response into out.
func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPut, path, nil, body, out)
}

// Delete issues a DELETE. Aikestra answers 204 on success.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil, nil)
}

// getPage fetches one page of a list endpoint. limit is clamped to
// 1–100 (0 takes the server default); cursor is the previous page's
// next_cursor, or "" for the first page.
func getPage[T any](ctx context.Context, c *Client, path string, query url.Values, limit int, cursor string) (*listResponse[T], error) {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(clampLimit(limit)))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var page listResponse[T]
	if err := c.Get(ctx, path, q, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// walk follows next_cursor from the first page until it is null,
// returning every item. Used where gaia's contract returns a whole
// collection (comments, relationships, refs) rather than one page.
// truncated reports that a page carried `truncated: true` or that the
// walk hit maxWalkPages before the list ended.
func walk[T any](ctx context.Context, c *Client, path string, query url.Values) (items []T, truncated bool, err error) {
	cursor := ""
	for i := 0; i < maxWalkPages; i++ {
		page, err := getPage[T](ctx, c, path, query, maxLimit, cursor)
		if err != nil {
			return nil, false, err
		}
		items = append(items, page.Items...)
		truncated = truncated || page.Truncated
		if page.NextCursor == nil {
			return items, truncated, nil
		}
		cursor = *page.NextCursor
	}
	return items, true, nil
}

// clampLimit bounds gaia's --limit to the server's 1–100 range.
func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultLimit
	case limit > maxLimit:
		return maxLimit
	}
	return limit
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("marshal %s %s body", method, path))
		}
	}
	urlStr := c.buildURL(path, query)

	resp, err := c.send(ctx, method, urlStr, raw)
	if err != nil {
		return exitcode.Wrap(scrubError(err, c.token), exitcode.Network, fmt.Sprintf("%s %s", method, path))
	}
	if method == http.MethodGet && resp.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		select {
		case <-time.After(c.retryWait):
		case <-ctx.Done():
			return exitcode.Wrap(ctx.Err(), exitcode.Network, fmt.Sprintf("%s %s (canceled before retry)", method, path))
		}
		resp, err = c.send(ctx, method, urlStr, nil)
		if err != nil {
			return exitcode.Wrap(scrubError(err, c.token), exitcode.Network, fmt.Sprintf("%s %s (retry)", method, path))
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError(resp, method, path)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("decode %s %s", method, path))
	}
	return nil
}

func (c *Client) send(ctx context.Context, method, urlStr string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, reader)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func (c *Client) buildURL(path string, query url.Values) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// statusError decodes Aikestra's `{"error":{"code","message"}}` body and
// maps it onto a gaia exit code via [mapError]. A body that isn't the
// documented shape (a proxy's HTML 502, say) falls back to the status.
func statusError(resp *http.Response, method, path string) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	var e errorResponse
	if json.Unmarshal(raw, &e) != nil || e.Error.Code == "" {
		snippet := strings.TrimSpace(string(raw))
		if snippet == "" {
			snippet = http.StatusText(resp.StatusCode)
		}
		return exitcode.Wrap(
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet),
			exitcode.FromHTTP(resp.StatusCode),
			fmt.Sprintf("%s %s", method, path),
		)
	}
	code, hint := mapError(resp.StatusCode, e.Error.Code)
	msg := e.Error.Message
	if hint != "" {
		msg += " (" + hint + ")"
	}
	return exitcode.Wrap(
		fmt.Errorf("HTTP %d %s: %s", resp.StatusCode, e.Error.Code, msg),
		code,
		fmt.Sprintf("%s %s", method, path),
	)
}

// mapError maps an Aikestra error code onto a gaia exit code, plus an
// optional hint for the operator. Clients match on `code`, never on the
// message. Unknown codes fall back to the HTTP status.
//
// 404 `not_found` also means "no access": Aikestra never reveals that a
// resource exists to someone who can't see it, so a 404 is never
// reported as forbidden. 403 `forbidden` is only for something visible
// that the caller may not change.
func mapError(status int, code string) (exit int, hint string) {
	switch code {
	case "invalid_request", "invalid_fields", "invalid_cursor",
		"method_not_allowed", "unsupported_media_type", "validation_failed":
		return exitcode.Usage, ""
	case "note_required":
		return exitcode.Usage, "this Project requires a note: pass --note"
	case "unauthenticated":
		return exitcode.Auth, "check AIKESTRA_TOKEN or run `gaia auth aikestra <api-url>`"
	case "forbidden":
		return exitcode.Auth, ""
	case "not_found":
		return exitcode.NotFound, "or not visible to this token"
	case "blocked":
		return exitcode.PolicyViolation, ""
	case "conflict", "cycle":
		return exitcode.Generic, ""
	case "internal_error":
		return exitcode.Network, ""
	}
	return exitcode.FromHTTP(status), ""
}

// scrubError defensively strips the token out of a transport-level
// error string.
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
