package types

// Branch is a trimmed git branch ref. Commit is the tip commit SHA;
// Protected reflects whether a branch-protection rule applies (both
// forges surface it on the branch object). URLs, the full commit
// object, and verification metadata the forges carry are dropped at the
// trim boundary.
type Branch struct {
	Name      string `json:"name"`
	Commit    string `json:"commit,omitempty"`
	Protected bool   `json:"protected,omitempty"`
}
