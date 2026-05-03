// Package envelope wraps every gaia response in a stable, versioned
// shape: {schema_version, data, _truncated?, _next_cursor?, _meta?}.
// CLI and MCP frontends both use it; the field-projection helper here
// applies --fields to the data subtree without disturbing the
// envelope's own meta fields.
//
// The wire shape is locked and documented in docs/output-format.md;
// breaking changes bump core/types.SchemaVersion.
package envelope

import (
	"encoding/json"
	"fmt"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// DefaultLimit is the default page size used by callers that don't
// specify their own. CLI subcommands honor this when --limit is
// omitted.
const DefaultLimit = 30

// MaxLimit caps the page size a caller can request. Past this point
// the request is silently clamped — the envelope's _truncated flag
// tells the caller they did not see everything.
const MaxLimit = 200

// Envelope is the wrapping value every gaia operation returns. Data
// holds the operation result; the underscore-prefixed fields carry
// pagination and operational metadata, omitted when empty.
type Envelope struct {
	SchemaVersion string         `json:"schema_version"`
	Data          any            `json:"data"`
	Truncated     bool           `json:"_truncated,omitempty"`
	NextCursor    string         `json:"_next_cursor,omitempty"`
	Meta          map[string]any `json:"_meta,omitempty"`
}

// envelopeJSON is the on-the-wire shape used by MarshalJSON. Same
// fields as Envelope but without the custom marshaler so json.Marshal
// doesn't recurse infinitely.
type envelopeJSON struct {
	SchemaVersion string         `json:"schema_version"`
	Data          any            `json:"data"`
	Truncated     bool           `json:"_truncated,omitempty"`
	NextCursor    string         `json:"_next_cursor,omitempty"`
	Meta          map[string]any `json:"_meta,omitempty"`
}

// MarshalJSON applies trust-tag rewriting to Data before standard
// JSON encoding. Fields tagged `gaia:"trust=external"` (issue
// bodies, comments, wiki content, etc.) are emitted as
// `{"_trust": "external", "_value": "<text>"}` so agents can
// distinguish operator-supplied input from forge-supplied content
// (#146 / docs/agent-guide.md threat model).
//
// Untagged data passes through verbatim, so operations that don't
// return user-provided text (whoami, label list, version, etc.) keep
// their existing wire shape unchanged.
func (e *Envelope) MarshalJSON() ([]byte, error) {
	out := envelopeJSON{
		SchemaVersion: e.SchemaVersion,
		Data:          applyTrustTags(e.Data),
		Truncated:     e.Truncated,
		NextCursor:    e.NextCursor,
		Meta:          e.Meta,
	}
	return json.Marshal(out)
}

// New constructs an Envelope around data with the current
// SchemaVersion stamped in.
func New(data any) *Envelope {
	return &Envelope{
		SchemaVersion: types.SchemaVersion,
		Data:          data,
	}
}

// WithPage threads pagination state from a Provider list-call into the
// envelope. Nil and zero-value pages leave the envelope's truncation
// fields at their defaults.
func (e *Envelope) WithPage(p *provider.Page) *Envelope {
	if p == nil {
		return e
	}
	e.Truncated = p.Truncated
	e.NextCursor = p.NextCursor
	return e
}

// WithMeta attaches an operational metadata key (rate-limit remaining,
// cache state, etc.) under _meta. Multiple calls accumulate.
func (e *Envelope) WithMeta(key string, value any) *Envelope {
	if e.Meta == nil {
		e.Meta = map[string]any{}
	}
	e.Meta[key] = value
	return e
}

// Project applies a field spec like "number,title,labels.name" to the
// Data subtree, leaving SchemaVersion, _truncated, _next_cursor, and
// _meta untouched. Empty spec is a no-op.
//
// The implementation round-trips Data through JSON so the spec can
// match the wire shape rather than Go field names — agents type
// `--fields created_at,labels.name`, not `--fields CreatedAt,Labels.Name`.
//
// Trust-tag preservation (#146): trust-tag rewriting is applied
// BEFORE the JSON round-trip so a projected field carrying external
// content keeps its `_trust` marker. Without this ordering, Project
// would convert tagged structs to plain map[string]any and the
// MarshalJSON-time walker would have no struct shape left to find
// the tag on.
func (e *Envelope) Project(spec string) error {
	fs := ParseFields(spec)
	if len(fs) == 0 {
		return nil
	}
	tagged := applyTrustTags(e.Data)
	raw, err := json.Marshal(tagged)
	if err != nil {
		return fmt.Errorf("envelope: marshal data for projection: %w", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return fmt.Errorf("envelope: unmarshal data for projection: %w", err)
	}
	e.Data = fs.Apply(tree)
	return nil
}
