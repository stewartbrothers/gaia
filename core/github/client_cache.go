package github

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

// GetCached is the cache-aware GET. See core/forgejo's GetCached for
// the full contract (#42); GitHub's auth/header conventions differ
// but the conditional-GET protocol is identical (RFC 7232).
func (c *Client) GetCached(ctx context.Context, path string, out any, key cache.Key, ttl time.Duration) error {
	if c.cache == nil {
		return c.Get(ctx, path, out)
	}

	hit, ok, err := c.cache.Lookup(ctx, key)
	if err == nil && ok && !hit.Stale {
		if out != nil {
			if err := json.Unmarshal(hit.Payload, out); err != nil {
				_ = c.cache.Invalidate(ctx, key)
				return c.fetchAndStore(ctx, path, out, key, ttl)
			}
		}
		return nil
	}

	resp, err := c.doConditional(ctx, path, hit, ok)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotModified && ok:
		if out != nil {
			if err := json.Unmarshal(hit.Payload, out); err != nil {
				_ = c.cache.Invalidate(ctx, key)
				return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("decode cached %s", path))
			}
		}
		if err := c.cache.Touch(ctx, key, time.Now()); err != nil {
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
			trimmed, err := json.Marshal(out)
			if err != nil {
				return exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("marshal trimmed %s", path))
			}
			etag := resp.Header.Get("ETag")
			lm := resp.Header.Get("Last-Modified")
			_ = c.cache.Store(ctx, cache.Entry{
				Key:          key,
				ETag:         etag,
				LastModified: lm,
				FetchedAt:    time.Now(),
				TTL:          ttl,
				Payload:      trimmed,
			})
		}
		return nil

	default:
		return c.statusError(resp, http.MethodGet, path)
	}
}

func (c *Client) fetchAndStore(ctx context.Context, path string, out any, key cache.Key, ttl time.Duration) error {
	resp, err := c.do(ctx, http.MethodGet, path, defaultAccept, nil)
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
		_ = c.cache.Store(ctx, cache.Entry{
			Key:  key,
			ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified"),
			FetchedAt: time.Now(), TTL: ttl, Payload: trimmed,
		})
	}
	return nil
}

func (c *Client) doConditional(ctx context.Context, path string, hit cache.Entry, have bool) (*http.Response, error) {
	urlStr := c.buildURL(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, fmt.Sprintf("build GET %s", path))
	}
	c.setHeaders(req, defaultAccept, false)
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
		c.setHeaders(req2, defaultAccept, false)
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
