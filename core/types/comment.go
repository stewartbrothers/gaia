package types

import "time"

// Comment is the unified comment shape across the three forge endpoints
// (issue comments, PR review comments, inline review comments). Source
// disambiguates; Path and Line are only set when Source == "inline".
type Comment struct {
	ID        int64     `json:"id"`
	Source    string    `json:"source"`
	Author    User      `json:"author"`
	Body      string    `json:"body"`
	Path      string    `json:"path,omitempty"`
	Line      int       `json:"line,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
