package types

import "time"

// Comment is the unified comment shape across the three forge endpoints
// (issue comments, PR review comments, inline review comments). Source
// disambiguates; Path and Line are only set when Source == "inline".
//
// Body carries forge-supplied user content and is tagged
// `gaia:"trust=external"` so the envelope marshaler emits it with a
// `_trust` marker (#146).
type Comment struct {
	ID        int64     `json:"id"`
	Source    string    `json:"source"`
	Author    User      `json:"author"`
	Body      string    `json:"body" gaia:"trust=external"`
	Path      string    `json:"path,omitempty"`
	Line      int       `json:"line,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
