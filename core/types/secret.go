package types

import "time"

// Secret is a CI/Actions secret's *metadata* — never its value. Both
// forges' APIs are write-only: a list call returns the secret's name and
// timestamps but no value, which is exactly what "what secrets are
// configured here" needs. Forgejo populates CreatedAt only; GitHub
// populates both. Pointers + omitempty so a forge that omits a timestamp
// doesn't emit a zero date.
type Secret struct {
	Name      string     `json:"name"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}
