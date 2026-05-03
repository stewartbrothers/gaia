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

// apiHookDelivery mirrors Forgejo's hook-task / delivery record. The
// upstream field set is large; we decode only what flows into
// WebhookDelivery + WebhookDeliveryDetail.
//
// `Duration` is seconds-as-float on the wire (Forgejo passes Go's
// time.Duration through json.Marshal, which renders it as
// nanoseconds-as-int64 — but the historical Gitea/Forgejo task type
// has used both float-seconds and int-nanoseconds depending on
// version). We sniff: <1e6 → seconds-as-float; otherwise → ns-as-int.
type apiHookDelivery struct {
	ID              int64             `json:"id"`
	Event           string            `json:"event"`
	Action          string            `json:"action"`
	Status          int               `json:"status"`
	ResponseStatus  int               `json:"response_status"`
	Duration        float64           `json:"duration"`
	IsRedelivery    bool              `json:"is_redelivery"`
	Delivered       string            `json:"delivered"`
	DeliveredAt     string            `json:"delivered_at"`
	RequestHeaders  map[string]string `json:"request_headers"`
	RequestBody     string            `json:"request_body"`
	ResponseHeaders map[string]string `json:"response_headers"`
	ResponseBody    string            `json:"response_body"`
}

func (a *apiHookDelivery) toType() types.WebhookDelivery {
	status := a.ResponseStatus
	if status == 0 {
		status = a.Status
	}
	return types.WebhookDelivery{
		ID:          a.ID,
		Event:       a.Event,
		Action:      a.Action,
		StatusCode:  status,
		DurationMs:  durationToMs(a.Duration),
		DeliveredAt: parseDeliveredAt(a.DeliveredAt, a.Delivered),
		Redelivery:  a.IsRedelivery,
	}
}

func (a *apiHookDelivery) toDetail() types.WebhookDeliveryDetail {
	return types.WebhookDeliveryDetail{
		WebhookDelivery: a.toType(),
		RequestHeaders:  a.RequestHeaders,
		RequestBody:     a.RequestBody,
		ResponseHeaders: a.ResponseHeaders,
		ResponseBody:    a.ResponseBody,
	}
}

// durationToMs normalizes Forgejo's two historical `duration` shapes
// to integer milliseconds: <1e6 → seconds-as-float (so 1.5 → 1500ms);
// >=1e6 → nanoseconds-as-int (so 1500000000 → 1500ms). The 1e6
// threshold sits in dead air between the two — no real webhook
// completes in <1ms (1e6ns) and none take ≥1e6 seconds (~11.5 days).
func durationToMs(d float64) int64 {
	if d <= 0 {
		return 0
	}
	if d < 1e6 {
		return int64(d * 1000)
	}
	return int64(d / 1e6)
}

// parseDeliveredAt prefers the RFC3339 string in `delivered_at`;
// older Forgejo versions used `delivered` for the same payload.
// Empty → zero time.
func parseDeliveredAt(primary, fallback string) time.Time {
	for _, s := range []string{primary, fallback} {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ListWebhookDeliveries returns recent delivery summaries for the
// hook. Bodies are NOT inlined — fetch GetWebhookDelivery for the
// full per-delivery payload.
func (p *Provider) ListWebhookDeliveries(ctx context.Context, owner, repo string, id int64, opts provider.ListDeliveriesOptions) ([]types.WebhookDelivery, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/hooks/%d/deliveries?%s", owner, repo, id, q.Encode())
	var raw []apiHookDelivery
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.WebhookDelivery, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetWebhookDelivery fetches one delivery's full request + response
// payload by delivery ID.
func (p *Provider) GetWebhookDelivery(ctx context.Context, owner, repo string, id, deliveryID int64) (*types.WebhookDeliveryDetail, error) {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d/deliveries/%d", owner, repo, id, deliveryID)
	var raw apiHookDelivery
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := raw.toDetail()
	return &out, nil
}

// RedeliverWebhook re-fires a previously-sent delivery. Forgejo
// expects POST to the delivery's resource URL; success is 204.
func (p *Provider) RedeliverWebhook(ctx context.Context, owner, repo string, id, deliveryID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d/deliveries/%d", owner, repo, id, deliveryID)
	return p.client.Post(ctx, path, nil, nil)
}

// TestWebhook sends a synthetic ping payload so the operator can
// confirm the receiver is reachable. Forgejo: POST /tests; success
// is 204 with no body.
func (p *Provider) TestWebhook(ctx context.Context, owner, repo string, id int64) error {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d/tests", owner, repo, id)
	return p.client.Post(ctx, path, nil, nil)
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
