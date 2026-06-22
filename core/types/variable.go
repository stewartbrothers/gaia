package types

import "time"

// Variable is a CI/Actions variable — non-secret configuration whose
// VALUE the API returns (unlike a Secret, which is write-only). A list
// call returns the variable's name, value, and timestamps. Forgejo
// populates CreatedAt/UpdatedAt only when present; GitHub populates
// both. Pointers + omitempty so a forge that omits a timestamp doesn't
// emit a zero date.
type Variable struct {
	Name      string     `json:"name"`
	Value     string     `json:"value"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}
