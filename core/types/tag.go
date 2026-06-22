package types

// Tag is a trimmed git tag ref. Commit is the SHA the tag points at;
// Message carries an annotated tag's message (empty for lightweight
// tags). URLs, the full commit object, the tagger, and verification
// metadata the forges carry are dropped at the trim boundary.
type Tag struct {
	Name    string `json:"name"`
	Commit  string `json:"commit,omitempty"`
	Message string `json:"message,omitempty"`
}
