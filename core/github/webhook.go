// Package github: webhook CRUD + delivery history (#85).
//
// GitHub's webhook surface lives at `/repos/{o}/{r}/hooks` with the
// same overall shape as Forgejo but a few field-name differences:
//
//   - Hook record carries `name:"web"` (Forgejo uses `type:"gitea"`).
//   - Config is nested the same way (`config.url`, `config.content_type`).
//   - Edit endpoint accepts `add_events` + `remove_events` directly,
//     so gaia does NOT need the pre-fetch-and-merge dance Forgejo
//     requires.
//   - Delivery redeliver endpoint is `.../deliveries/{id}/attempts`
//     (Forgejo uses `.../deliveries/{id}` with POST).
//   - Delivery payload nests request/response bodies under top-level
//     `request`/`response` objects with `headers` + `payload` keys.
//
// gaia maps both forges into the unified `types.Webhook` /
// `types.WebhookDelivery{Detail}` shapes at the trim boundary.
package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiHook mirrors GitHub's hook record (the fields gaia trims into
// Webhook). Fields not listed are dropped at decode.
type apiHook struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Config    apiHookConfig `json:"config"`
	Events    []string      `json:"events"`
	Active    bool          `json:"active"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type apiHookConfig struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	// Secret intentionally omitted (GitHub redacts on read).
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

// hookCreatePayload is GitHub's POST body shape: {name, config,
// events, active}. name="web" is the only valid value for repo-level
// hooks.
type hookCreatePayload struct {
	Name   string            `json:"name"`
	Config hookConfigPayload `json:"config"`
	Events []string          `json:"events"`
	Active bool              `json:"active"`
}

type hookConfigPayload struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Secret      string `json:"secret,omitempty"`
}

// hookPatchPayload is GitHub's PATCH body shape. Unlike Forgejo,
// GitHub accepts `add_events` + `remove_events` directly so gaia
// passes them through verbatim — no pre-fetch round-trip required.
type hookPatchPayload struct {
	Config       *hookConfigPayload `json:"config,omitempty"`
	Events       []string           `json:"events,omitempty"`
	AddEvents    []string           `json:"add_events,omitempty"`
	RemoveEvents []string           `json:"remove_events,omitempty"`
	Active       *bool              `json:"active,omitempty"`
}

// ListWebhooks returns webhooks configured on the repo.
func (p *Provider) ListWebhooks(ctx context.Context, owner, repo string, opts provider.ListWebhooksOptions) ([]types.Webhook, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
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

// GetWebhook fetches one webhook by ID.
func (p *Provider) GetWebhook(ctx context.Context, owner, repo string, id int64) (*types.Webhook, error) {
	var raw apiHook
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, id)
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// CreateWebhook installs a new webhook.
func (p *Provider) CreateWebhook(ctx context.Context, owner, repo string, opts provider.CreateWebhookOptions) (*types.Webhook, error) {
	body := hookCreatePayload{
		Name: "web",
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

// EditWebhook patches a webhook by ID. AddEvents and RemoveEvents
// pass through to GitHub's `add_events`/`remove_events` body fields
// — GitHub does the merge server-side so gaia avoids a pre-fetch
// (Forgejo's edit path needs the round-trip; GitHub's doesn't).
func (p *Provider) EditWebhook(ctx context.Context, owner, repo string, id int64, opts provider.EditWebhookOptions) (*types.Webhook, error) {
	patch := hookPatchPayload{
		AddEvents:    opts.AddEvents,
		RemoveEvents: opts.RemoveEvents,
		Active:       opts.Active,
	}
	if opts.URL != "" || opts.ContentType != "" || opts.Secret != "" {
		cfg := &hookConfigPayload{
			URL:         opts.URL,
			ContentType: opts.ContentType,
			Secret:      opts.Secret,
		}
		patch.Config = cfg
	}

	var raw apiHook
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, id)
	if err := p.client.Patch(ctx, path, patch, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeleteWebhook removes a webhook by ID. 204 success.
func (p *Provider) DeleteWebhook(ctx context.Context, owner, repo string, id int64) error {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, id)
	return p.client.Delete(ctx, path)
}

// apiHookDelivery is GitHub's delivery record. Status code lives at
// the top level (`status_code`); the human-readable status string is
// `status`. Duration is seconds-as-float.
type apiHookDelivery struct {
	ID          int64     `json:"id"`
	Event       string    `json:"event"`
	Action      string    `json:"action"`
	StatusCode  int       `json:"status_code"`
	Duration    float64   `json:"duration"`
	Redelivery  bool      `json:"redelivery"`
	DeliveredAt time.Time `json:"delivered_at"`

	Request  *apiHookDeliveryDir `json:"request,omitempty"`
	Response *apiHookDeliveryDir `json:"response,omitempty"`
}

// apiHookDeliveryDir is one direction (request OR response) on a
// detailed delivery payload.
type apiHookDeliveryDir struct {
	Headers map[string]string `json:"headers"`
	Payload string            `json:"payload"`
}

func (a *apiHookDelivery) toType() types.WebhookDelivery {
	return types.WebhookDelivery{
		ID:          a.ID,
		Event:       a.Event,
		Action:      a.Action,
		StatusCode:  a.StatusCode,
		DurationMs:  int64(a.Duration * 1000),
		DeliveredAt: a.DeliveredAt,
		Redelivery:  a.Redelivery,
	}
}

func (a *apiHookDelivery) toDetail() types.WebhookDeliveryDetail {
	out := types.WebhookDeliveryDetail{WebhookDelivery: a.toType()}
	if a.Request != nil {
		out.RequestHeaders = a.Request.Headers
		out.RequestBody = a.Request.Payload
	}
	if a.Response != nil {
		out.ResponseHeaders = a.Response.Headers
		out.ResponseBody = a.Response.Payload
	}
	return out
}

// ListWebhookDeliveries returns recent delivery summaries.
func (p *Provider) ListWebhookDeliveries(ctx context.Context, owner, repo string, id int64, opts provider.ListDeliveriesOptions) ([]types.WebhookDelivery, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
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

// GetWebhookDelivery fetches one delivery's full payload.
func (p *Provider) GetWebhookDelivery(ctx context.Context, owner, repo string, id, deliveryID int64) (*types.WebhookDeliveryDetail, error) {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d/deliveries/%d", owner, repo, id, deliveryID)
	var raw apiHookDelivery
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := raw.toDetail()
	return &out, nil
}

// RedeliverWebhook re-fires a previously-sent delivery. GitHub's
// path is `.../deliveries/{id}/attempts` (Forgejo uses `.../deliveries/{id}`).
// Async on the server side — 202 Accepted is success.
func (p *Provider) RedeliverWebhook(ctx context.Context, owner, repo string, id, deliveryID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d/deliveries/%d/attempts", owner, repo, id, deliveryID)
	if err := p.client.Post(ctx, path, nil, nil); err != nil {
		// 202 falls through as success in the client layer; surface
		// any other error as-is.
		return err
	}
	return nil
}

// TestWebhook sends a synthetic ping. GitHub's test endpoint
// dispatches a `push` event using the repo's most recent commit;
// 204 No Content is success.
func (p *Provider) TestWebhook(ctx context.Context, owner, repo string, id int64) error {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d/tests", owner, repo, id)
	return p.client.Post(ctx, path, nil, nil)
}
