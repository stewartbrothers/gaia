package github

import "testing"

// uploadHostFor() is unexported; tested in-package because the
// mapping is the load-bearing detail for GitHub asset uploads (which
// go to a separate host from the API). Cheap to lock down.
func TestUploadHostFor(t *testing.T) {
	cases := []struct {
		name string
		api  string
		want string
	}{
		{"github.com", "https://api.github.com", "https://uploads.github.com"},
		{"GHES", "https://api.example.com/api/v3", "https://api.example.com/api/uploads"},
		{"unknown falls through to caller's base", "https://forge.local", "https://forge.local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uploadHostFor(tc.api); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
