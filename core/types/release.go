package types

import "time"

// ReleaseAsset is a file attached to a release.
type ReleaseAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size,omitempty"`
}

// Release is the trimmed view of a forge release. Asset upload/download
// is intentionally NOT included on this type — that's a separate
// streaming flow that benefits from its own treatment in Phase 4.
//
// TargetCommitish is the branch or commit SHA the release was cut
// against; populated by the forge but optional in create flows.
type Release struct {
	ID              int64      `json:"-"`
	TagName         string     `json:"tag_name"`
	Name            string     `json:"name,omitempty" gaia:"trust=external"`
	Body            string     `json:"body,omitempty" gaia:"trust=external"`
	Draft           bool       `json:"draft"`
	Prerelease      bool       `json:"prerelease"`
	Author          User       `json:"author"`
	TargetCommitish string     `json:"target_commitish,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}
