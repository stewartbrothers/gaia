package types

// Runner is a self-hosted Actions runner's *operational* status —
// enough to answer "is this CI runner online, and what can it run". Name
// identifies the runner; Status is the forge's online/offline string;
// Busy reports whether it's mid-job; Labels are the runner's capability
// tags (both forges flatten to a plain string slice — GitHub's nested
// {name} objects are flattened at the wire boundary). Internal IDs, the
// OS string, and other API bloat are deliberately omitted.
type Runner struct {
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Busy   bool     `json:"busy"`
	Labels []string `json:"labels,omitempty"`
}
