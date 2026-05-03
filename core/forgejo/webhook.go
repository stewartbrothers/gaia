// Package forgejo: webhook CRUD + delivery history (#85).
//
// Forgejo's webhook surface lives at `/repos/{o}/{r}/hooks` and carries
// a slightly old-school shape: a top-level `events` array, a top-level
// `active`, but the URL/content_type/secret nest under `config`. Read
// responses redact the secret (`config.secret` is empty); write
// requests carry it under the same key.
//
// gaia maps both into the unified `types.Webhook` (URL +
// ContentType promoted to top-level) at the trim boundary.
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

// apiHook mirrors Forgejo's hook record. We only decode what we
// trim into Webhook; everything else is dropped.
type apiHook struct {
	ID        int64         `json:"id"`
	Type      string        `json:"type"`
	Config    apiHookConfig `json:"config"`
	Events    []string      `json:"events"`
	Active    bool          `json:"active"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type apiHookConfig struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	// Secret intentionally omitted; Forgejo redacts it on read and
	// we never want to decode-then-stash a secret-shaped string.
}

func (a *apiHook) toType() types.Webhook {
	return types.Webhook{
		ID:          a.ID,
		URL:         a.Config.URL,
		ContentType: a.Config.ContentType,
		Events:      append([]string(nil), a.Events...),
		Active:      a.Active,
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}
}

// hookCreatePayload is the wire shape Forgejo expects for
// POST /repos/{o}/{r}/hooks. type=gitea is the default Forgejo flavour
// (the alternative `gogs` etc. exist but aren't relevant here).
type hookCreatePayload struct {
	Type   string            `json:"type"`
	Config hookConfigPayload `json:"config"`
	Events []string          `json:"events"`
	Active bool              `json:"active"`
}

type hookConfigPayload struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Secret      string `json:"secret,omitempty"`
}

// hookPatchPayload is what Forgejo expects for
// PATCH /repos/{o}/{r}/hooks/{id}. Empty fields and nil pointers are
// dropped (omitempty) so the request only mutates what the caller
// actually wanted to change.
type hookPatchPayload struct {
	Config *hookConfigPayload `json:"config,omitempty"`
	Events []string           `json:"events,omitempty"`
	Active *bool              `json:"active,omitempty"`
}

// ListWebhooks returns all configured webhooks for the repo. Forgejo
// paginates these the same way it paginates other list endpoints.
func (p *Provider) ListWebhooks(ctx context.Context, owner, repo string, opts provider.ListWebhooksOptions) ([]types.Webhook, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/hooks?%s", owner, repo, q.Encode())
	var raw []apiHook
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Webhook, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetWebhook fetches one hook by ID.
func (p *Provider) GetWebhook(ctx context.Context, owner, repo string, id int64) (*types.Webhook, error) {
	var raw apiHook
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, id)
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// CreateWebhook installs a new webhook. Secret (if non-empty) is
// passed through in the create body — the only place it travels.
func (p *Provider) CreateWebhook(ctx context.Context, owner, repo string, opts provider.CreateWebhookOptions) (*types.Webhook, error) {
	body := hookCreatePayload{
		Type: "gitea",
		Config: hookConfigPayload{
			URL:         opts.URL,
			ContentType: opts.ContentType,
			Secret:      opts.Secret,
		},
		Events: opts.Events,
		Active: opts.Active,
	}
	var raw apiHook
	path := fmt.Sprintf("/repos/%s/%s/hooks", owner, repo)
	if err := p.client.Post(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// EditWebhook patches an existing webhook by ID. AddEvents and
// RemoveEvents apply incrementally — gaia pre-fetches the current
// event list, computes the merged set, and PATCHes the result. This
// matches the contract documented on EditWebhookOptions and means
// callers don't need to fetch-and-replace by hand.
func (p *Provider) EditWebhook(ctx context.Context, owner, repo string, id int64, opts provider.EditWebhookOptions) (*types.Webhook, error) {
	patch := hookPatchPayload{Active: opts.Active}

	// Build the config sub-object only if the caller actually wants
	// to change at least one config field. Empty config is dropped
	// via omitempty so it won't show up in the wire body.
	if opts.URL != "" || opts.ContentType != "" || opts.Secret != "" {
		cfg := &hookConfigPayload{
			URL:         opts.URL,
			ContentType: opts.ContentType,
			Secret:      opts.Secret,
		}
		patch.Config = cfg
	}

	// Resolve the merged event list if either Add or Remove was set.
	if len(opts.AddEvents) > 0 || len(opts.RemoveEvents) > 0 {
		current, err := p.GetWebhook(ctx, owner, repo, id)
		if err != nil {
			return nil, err
		}
		patch.Events = mergeEvents(current.Events, opts.AddEvents, opts.RemoveEvents)
	}

	var raw apiHook
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, id)
	if err := p.client.Patch(ctx, path, patch, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeleteWebhook removes a webhook by ID. 204 is success.
func (p *Provider) DeleteWebhook(ctx context.Context, owner, repo string, id int64) error {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, id)
	return p.client.Delete(ctx, path)
}

// Delivery + redeliver + test impls land in commit 3 of this stack.
// Stub here keeps the Provider interface satisfied at this commit
// boundary so the rest of the build stays green.

// ListWebhookDeliveries is implemented in a follow-up commit.
func (p *Provider) ListWebhookDeliveries(_ context.Context, _, _ string, _ int64, _ provider.ListDeliveriesOptions) ([]types.WebhookDelivery, *provider.Page, error) {
	return nil, nil, fmt.Errorf("forgejo: ListWebhookDeliveries not yet implemented")
}

// GetWebhookDelivery is implemented in a follow-up commit.
func (p *Provider) GetWebhookDelivery(_ context.Context, _, _ string, _, _ int64) (*types.WebhookDeliveryDetail, error) {
	return nil, fmt.Errorf("forgejo: GetWebhookDelivery not yet implemented")
}

// RedeliverWebhook is implemented in a follow-up commit.
func (p *Provider) RedeliverWebhook(_ context.Context, _, _ string, _, _ int64) error {
	return fmt.Errorf("forgejo: RedeliverWebhook not yet implemented")
}

// TestWebhook is implemented in a follow-up commit.
func (p *Provider) TestWebhook(_ context.Context, _, _ string, _ int64) error {
	return fmt.Errorf("forgejo: TestWebhook not yet implemented")
}

// mergeEvents applies +add / -remove to the existing list, preserving
// the original order for stability and dropping duplicates.
func mergeEvents(existing, add, remove []string) []string {
	out := make([]string, 0, len(existing)+len(add))
	seen := map[string]bool{}
	rm := map[string]bool{}
	for _, e := range remove {
		rm[e] = true
	}
	for _, e := range existing {
		if rm[e] || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	for _, e := range add {
		if rm[e] || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}
