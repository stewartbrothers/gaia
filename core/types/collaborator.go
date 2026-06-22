package types

// Collaborator is one user with read-or-better access to a repo, plus
// their effective permission level. It answers "who can touch this repo
// and at what level" — a repo access audit.
//
// Permission is the forge's effective access level for the user. GitHub
// exposes it inline on the collaborators list (role_name / a permissions
// map); Forgejo's list omits it, so the Forgejo provider resolves it with
// one extra per-user call. Permission is omitempty so a forge (or a v1
// path) that can't supply it emits Login alone rather than an empty
// string field.
type Collaborator struct {
	Login      string `json:"login"`
	Permission string `json:"permission,omitempty"`
}
