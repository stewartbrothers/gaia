package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

// GetCached is the cache-aware counterpart to Get. It performs the
// conditional-GET dance:
//
//  1. Lookup the cache row at key. If present and fresh (fetched_at
//     within TTL), decode the cached payload into out and return —
//     zero upstream traffic.
//  2. If present but stale, attach `If-None-Match` (when the row
//     carries an ETag) and `If-Modified-Since` (when the row carries
//     a Last-Modified) to the upstream request.
//  3. On 304 Not Modified, decode the cached payload into out and
//     bump fetched_at via Touch — the upstream confirmed the cached
//     bytes are still current.
//  4. On 200, decode the upstream JSON into out, marshal the trimmed
//     value back to JSON, and replace the cache row (capturing the
//     new ETag and Last-Modified for next time).
//
// When the Client has no Cache (Options.Cache is nil) GetCached
// degrades to a plain Get — useful for the `--no-cache` path and
// for tests that want to reach upstream regardless.
//
// ttl is the entry's expiry: 5 min for single-resource reads (#42),
// 30s for list-style reads. Caller decides per call site so the same
// path can be cached with different freshness windows depending on
// shape.
func (c *Client) GetCached(ctx context.Context, path string, out any, key cache.Key, ttl time.Duration) error {
	if c.cache == nil {
		return c.Get(ctx, path, out)
	}

	hit, ok, err := c.cache.Lookup(ctx, key)
	if err == nil && ok && !hit.Stale {
		// Fresh — payload is good.
		if out != nil {
			if err := json.Unmarshal(hit.Payload, out); err != nil {
				// Cache is corrupt for this key. Bail to upstream
				// rather than returning a confusing error to the caller.
				_ = c.cache.Invalidate(ctx, key)
				return c.fetchAndStore(ctx, path, out, key, ttl)
			}
		}
		return nil
	}

	// Cache miss or stale: do an upstream call, possibly with
	// If-None-Match / If-Modified-Since.
	resp, err := c.doConditional(ctx, path, hit, ok)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotModified && ok:
		// Upstream says cached payload still applies. Touch it,
		// decode the cached bytes into out.
		if out != nil {
			if err := json.Unmarshal(hit.Payload, out); err != nil {
				_ = c.cache.Invalidate(ctx, key)
				return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("decode cached %s", path))
			}
		}
		if err := c.cache.Touch(ctx, key, time.Now()); err != nil {
			// Touch failure isn't fatal — the data is good — but
			// surfacing it via a no-op log isn't great either. Best
			// effort.
			_ = c.cache.Invalidate(ctx, key)
		}
		return nil

	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return exitcode.Wrap(err, exitcode.Network, fmt.Sprintf("read %s", path))
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("decode %s %s", http.MethodGet, path))
			}
		}
		// Re-marshal the trimmed value (out) so we cache the
		// boundary-trimmed shape, not the raw forge JSON. If out is
		// nil the caller doesn't care about the payload — skip
		// caching, it gives no benefit.
		if out != nil {
			trimmed, err := json.Marshal(out)
			if err != nil {
				return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("marshal trimmed %s", path))
			}
			etag := resp.Header.Get("ETag")
			lm := resp.Header.Get("Last-Modified")
			if storeErr := c.cache.Store(ctx, cache.Entry{
				Key:          key,
				ETag:         etag,
				LastModified: lm,
				FetchedAt:    time.Now(),
				TTL:          ttl,
				Payload:      trimmed,
			}); storeErr != nil {
				// Cache write failures should never fail the read —
				// a degraded cache is better than a degraded UX.
				_ = storeErr // intentionally swallowed
			}
		}
		return nil

	default:
		return c.statusError(resp, http.MethodGet, path)
	}
}

// fetchAndStore is the simple "no useful cache row, just fetch" path,
// shared between corrupt-cache recovery and the cache-disabled mode
// when the caller still wants the read to land in the cache once
// fetched.
func (c *Client) fetchAndStore(ctx context.Context, path string, out any, key cache.Key, ttl time.Duration) error {
	resp, err := c.do(ctx, http.MethodGet, path, "application/json", nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(resp, http.MethodGet, path)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return exitcode.Wrap(err, exitcode.Network, fmt.Sprintf("read %s", path))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("decode %s", path))
		}
		trimmed, err := json.Marshal(out)
		if err != nil {
			return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("marshal %s", path))
		}
		etag := resp.Header.Get("ETag")
		lm := resp.Header.Get("Last-Modified")
		_ = c.cache.Store(ctx, cache.Entry{
			Key: key, ETag: etag, LastModified: lm,
			FetchedAt: time.Now(), TTL: ttl, Payload: trimmed,
		})
	}
	return nil
}

// doConditional issues a GET, optionally adding If-None-Match /
// If-Modified-Since headers from the cached entry. Pass have=false
// for a vanilla GET.
func (c *Client) doConditional(ctx context.Context, path string, hit cache.Entry, have bool) (*http.Response, error) {
	urlStr := c.buildURL(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("build GET %s", path))
	}
	c.setHeaders(req, "application/json", false)
	if have {
		if hit.ETag != "" {
			req.Header.Set("If-None-Match", hit.ETag)
		}
		if hit.LastModified != "" {
			req.Header.Set("If-Modified-Since", hit.LastModified)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, exitcode.Wrap(scrubError(err, c.token), exitcode.Network, fmt.Sprintf("GET %s", path))
	}
	// shouldRetry only fires on 5xx, never on 304 — so the existing
	// retry path is a one-shot here too. Reuse it for parity with Get.
	if shouldRetry(http.MethodGet, resp.StatusCode) {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		select {
		case <-time.After(c.retryWait):
		case <-ctx.Done():
			return nil, exitcode.Wrap(ctx.Err(), exitcode.Network, fmt.Sprintf("GET %s (canceled before retry)", path))
		}
		req2, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return nil, exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("rebuild GET %s for retry", path))
		}
		c.setHeaders(req2, "application/json", false)
		if have {
			if hit.ETag != "" {
				req2.Header.Set("If-None-Match", hit.ETag)
			}
			if hit.LastModified != "" {
				req2.Header.Set("If-Modified-Since", hit.LastModified)
			}
		}
		resp, err = c.httpClient.Do(req2)
		if err != nil {
			return nil, exitcode.Wrap(scrubError(err, c.token), exitcode.Network, fmt.Sprintf("GET %s (retry)", path))
		}
	}
	return resp, nil
}
