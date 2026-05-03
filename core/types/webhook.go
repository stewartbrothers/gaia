package types

import "time"

// Webhook is the trimmed view of a repo webhook (Forgejo "hook" /
// GitHub "hook"). Both forges expose CRUD, delivery history, and
// redeliver under the same `/repos/{o}/{r}/hooks` shape; gaia
// reconciles the two field-name dialects (Forgejo's flat
// `config_url`/`config_content_type` vs. GitHub's nested
// `config.url`/`config.content_type`) into this struct at the boundary.
//
// Secrets are deliberately NOT carried on this type. Both forges
// redact the secret value on read; only the create/edit option
// structs accept it. Avoiding a `Secret` field on the read shape
// keeps the trimmed type from looking like it might leak the
// signing key.
type Webhook struct {
	ID          int64     `json:"id"`
	URL         string    `json:"url"`
	ContentType string    `json:"content_type"`
	Events      []string  `json:"events"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WebhookDelivery is the trimmed summary of one webhook delivery
// attempt — what the dashboard's "Recent Deliveries" table shows.
// Raw request/response bodies are intentionally omitted from the
// list shape; fetch a WebhookDeliveryDetail to see them. Webhook
// payloads are commonly multi-KB JSON (push events with full
// commit lists, PR events with full PR objects), so a list of
// 30 deliveries with bodies inlined would overrun any sensible
// agent context budget.
//
// DurationMs records the full request duration in milliseconds
// — Forgejo and GitHub both express this in seconds (float) on
// the wire; gaia normalizes to integer milliseconds for stable
// JSON output. StatusCode is the HTTP response code the receiver
// returned (0 if the request never connected).
type WebhookDelivery struct {
	ID          int64     `json:"id"`
	Event       string    `json:"event"`
	Action      string    `json:"action,omitempty"`
	StatusCode  int       `json:"status_code"`
	DurationMs  int64     `json:"duration_ms"`
	DeliveredAt time.Time `json:"delivered_at"`
	Redelivery  bool      `json:"redelivery"`
}

// WebhookDeliveryDetail is the per-delivery deep-dive: same identifier
// fields as WebhookDelivery plus the captured request and response
// payloads. Use sparingly — a single delivery for a `push` event on
// a busy repo can be 50–200 KB.
//
// RequestHeaders and ResponseHeaders use `map[string]string` rather
// than `http.Header` because the wire shape is already flat
// (single-value) on both forges. RequestBody and ResponseBody are
// the verbatim JSON bytes (already-pretty-formatted by the forge in
// some cases; gaia does not re-format).
type WebhookDeliveryDetail struct {
	WebhookDelivery
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	RequestBody     string            `json:"request_body,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	ResponseBody    string            `json:"response_body,omitempty"`
}
