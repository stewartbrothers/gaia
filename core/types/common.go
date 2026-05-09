// Package types defines the trimmed, agent-shaped wire types that gaia's
// CLI and MCP frontends serialize. Every Provider implementation returns
// values of these types, and the JSON shape is the public contract — see
// docs/output-format.md (lands with #10) for the wrapping envelope.
//
// The deliberate omissions are as important as the fields that are here:
// no URLs, no avatar links, no upstream-internal IDs, no ETags. If a
// downstream consumer needs one, that's a separate, justified design
// decision; resist adding by reflex.
package types

// SchemaVersion is the version stamp written into the output envelope by
// callers. Bump on every breaking change to the wire shape; minor
// additive changes keep the version stable.
const SchemaVersion = "1.0"

// User identifies a forge user by login only. Avatars, full names, and
// internal IDs are intentionally omitted — agents branch on login.
type User struct {
	Login string `json:"login"`
}

// Label is the trimmed view of a forge label. For read paths the Name
// alone is what agents branch on, but Color/Description are useful
// for label CRUD (#g4) where the agent is creating or editing labels
// rather than just listing them. ID is the forge's numeric identifier;
// surfaced because some forge APIs (Forgejo's PATCH/DELETE) take it,
// and agents falling back to direct API calls otherwise have no way
// to discover it. All optional fields are omitempty so the read path
// stays compact when those fields aren't populated.
type Label struct {
	ID          int64  `json:"id,omitempty"`
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}
