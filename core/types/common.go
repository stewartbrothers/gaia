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

// Label is just the label name. Colors and IDs live on the forge; agents
// don't need them.
type Label struct {
	Name string `json:"name"`
}
