package settings

import (
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
)

// remoteFitsProvider decides whether the git remote's host can name a
// repo on the active forge (#412). The remote is a clue: it's used
// unless it points at a different forge. A false answer carries the
// reason `config doctor` prints.
func TestRemoteFitsProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		apiURL   string
		host     string
		others   []string
		want     bool
	}{
		{"aikestra remote on its own host", "aikestra", "http://localhost:8091/api/v1", "localhost", nil, true},
		{"aikestra vs forgejo remote", "aikestra", "http://localhost:8080/api/v1", "repo.stewartbrothers.com.au", nil, false},
		{"aikestra with no api url", "aikestra", "", "localhost", nil, false},
		{"forgejo split ssh/api hosts", "forgejo", "https://git.stewartbrothers.com.au/api/v1", "repo.stewartbrothers.com.au", []string{"localhost"}, true},
		{"forgejo vs github.com remote", "forgejo", "https://git.example.com/api/v1", "github.com", nil, false},
		{"forgejo vs another configured forge", "forgejo", "https://git.example.com/api/v1", "localhost", []string{"localhost"}, false},
		{"github.com remote on github", "github", "https://api.github.com", "github.com", nil, true},
		{"github default api url", "github", "", "github.com", nil, true},
		{"github vs forgejo remote", "github", "https://api.github.com", "repo.stewartbrothers.com.au", nil, false},
		{"github enterprise host", "github", "https://ghe.corp.example/api/v3", "ghe.corp.example", nil, true},
		{"unknown provider keeps old behaviour", "", "", "anything.example", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := remoteFitsProvider(c.provider, c.apiURL, c.host, c.others)
			if got != c.want {
				t.Fatalf("remoteFitsProvider(%q,%q,%q,%v) = %v (%q), want %v", c.provider, c.apiURL, c.host, c.others, got, reason, c.want)
			}
			if !got && reason == "" {
				t.Errorf("a rejected remote must carry a reason")
			}
		})
	}
}

func TestForgeForRemote(t *testing.T) {
	fj := storedLogin{"forgejo", "git.example.com", auth.Credential{APIURL: "https://git.example.com/api/v1"}}
	ak := storedLogin{"aikestra", "git.example.com:8091", auth.Credential{APIURL: "http://git.example.com:8091/api/v1"}}
	keyOnly := storedLogin{"forgejo", "code.example.org", auth.Credential{}}
	cases := []struct {
		name   string
		host   string
		logins []storedLogin
		want   string
		ok     bool
	}{
		{"one login on host", "git.example.com", []storedLogin{fj}, "forgejo", true},
		{"two forges on one host is ambiguous", "git.example.com", []storedLogin{fj, ak}, "", false},
		{"login with only a key host", "code.example.org", []storedLogin{keyOnly}, "forgejo", true},
		{"github.com with no login", "github.com", nil, "github", true},
		{"unknown host", "nowhere.example", []storedLogin{fj}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, ok := forgeForRemote(c.host, c.logins)
			if got != c.want || ok != c.ok {
				t.Errorf("forgeForRemote(%q) = (%q,%v), want (%q,%v)", c.host, got, ok, c.want, c.ok)
			}
		})
	}
}
