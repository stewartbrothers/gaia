package settings

import "testing"

// remoteFitsProvider decides whether a git remote's host can name a
// repo on the active provider (#412). A false answer carries the
// reason doctor reports.
func TestRemoteFitsProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		apiURL   string
		host     string
		want     bool
	}{
		{"aikestra never hosts git", "aikestra", "http://localhost:8080/api/v1", "repo.stewartbrothers.com.au", false},
		{"aikestra on same host still no", "aikestra", "https://a.example.com/api/v1", "a.example.com", false},
		{"forgejo split ssh/api hosts", "forgejo", "https://git.stewartbrothers.com.au/api/v1", "repo.stewartbrothers.com.au", true},
		{"forgejo vs github.com remote", "forgejo", "https://git.example.com/api/v1", "github.com", false},
		{"github.com remote on github", "github", "https://api.github.com", "github.com", true},
		{"github default api url", "github", "", "github.com", true},
		{"github vs forgejo remote", "github", "https://api.github.com", "repo.stewartbrothers.com.au", false},
		{"github enterprise host", "github", "https://ghe.corp.example/api/v3", "ghe.corp.example", true},
		{"unknown provider keeps old behaviour", "", "", "anything.example", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := remoteFitsProvider(c.provider, c.apiURL, c.host)
			if got != c.want {
				t.Fatalf("remoteFitsProvider(%q,%q,%q) = %v (%q), want %v", c.provider, c.apiURL, c.host, got, reason, c.want)
			}
			if !got && reason == "" {
				t.Errorf("a rejected remote must carry a reason")
			}
		})
	}
}
