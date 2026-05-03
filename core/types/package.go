package types

import "time"

// Package is the trimmed view of a forge package-registry record.
// Forgejo and GitHub both expose per-user/org packages across multiple
// registries (npm, maven, container, generic, ...); the shape here
// is the union, kept minimal so agents only see what they branch on.
//
// Owner is the user or organization that owns the package — packages
// live under user/org scope, not under a repo. Type names the registry
// (npm, maven, container, generic, ...). Size is optional because not
// every registry reports it on the per-version record (Forgejo's
// generic + container records do; npm's per-version size is on the
// version doc, not the package doc).
type Package struct {
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size,omitempty"`
}
