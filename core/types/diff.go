package types

// DiffFile is one file in a PR diff. Status takes one of "added",
// "modified", "removed", "renamed". For renamed files OldPath is the
// pre-rename path. Binary files marshal with Binary=true and no Hunks.
type DiffFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Status  string `json:"status"`
	Binary  bool   `json:"binary"`
	Hunks   []Hunk `json:"hunks,omitempty"`
}

// Hunk is a contiguous run of changed (and optionally context) lines in
// a DiffFile. Lines preserves the leading +/-/space marker so consumers
// can reconstruct the unified-diff representation if needed.
type Hunk struct {
	Header   string   `json:"header"`
	OldStart int      `json:"old_start"`
	OldLines int      `json:"old_lines"`
	NewStart int      `json:"new_start"`
	NewLines int      `json:"new_lines"`
	Lines    []string `json:"lines"`
}
