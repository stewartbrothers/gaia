package autodetect_test

import (
	"testing"

	"github.com/stewartbrothers/gaia/core/autodetect"
)

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		name, raw         string
		host, owner, repo string
	}{
		{"scp-github", "git@github.com:foo/bar.git", "github.com", "foo", "bar"},
		{"scp-no-suffix", "git@github.com:foo/bar", "github.com", "foo", "bar"},
		{"scp-self-hosted", "git@github.com:stewartbrothers/gaia.git", "github.com", "Gerwood", "gaia"},
		{"ssh-protocol", "ssh://git@github.com/foo/bar.git", "github.com", "foo", "bar"},
		{"ssh-with-port", "ssh://git@host.example:2222/foo/bar.git", "host.example", "foo", "bar"},
		{"https", "https://github.com/foo/bar.git", "github.com", "foo", "bar"},
		{"https-no-suffix", "https://github.com/stewartbrothers/gaia", "your-forge.example.com", "Gerwood", "gaia"},
		{"https-with-userinfo", "https://user:tok@git.example/foo/bar.git", "git.example", "foo", "bar"},
		{"git-protocol", "git://github.com/foo/bar.git", "github.com", "foo", "bar"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := autodetect.ParseRemoteURL(c.raw)
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q): %v", c.raw, err)
			}
			if got.Host != c.host || got.Owner != c.owner || got.Name != c.repo {
				t.Errorf("got host=%q owner=%q name=%q; want %q %q %q",
					got.Host, got.Owner, got.Name, c.host, c.owner, c.repo)
			}
		})
	}
}

func TestParseRemoteURLErrors(t *testing.T) {
	cases := []string{
		"",
		"not-a-url",
		"git@github.com",            // no path
		"https://github.com",        // no path
		"git@github.com:single",     // not owner/repo
		"https://github.com/single", // not owner/repo
		"git@github.com:a/b/c",      // too deep (no subgroup support)
		"https://github.com/a/b/c.git",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if _, err := autodetect.ParseRemoteURL(raw); err == nil {
				t.Errorf("expected error for %q; got nil", raw)
			}
		})
	}
}

func TestProviderFor(t *testing.T) {
	cases := []struct {
		host  string
		hosts []string
		want  string
	}{
		{"github.com", nil, "github"},
		{"your-forge.example.com", nil, "forgejo"},
		{"any.other.host", nil, "forgejo"},
		// Allowlist is preserved as a future hook; today it does not
		// change behavior (built-in github.com mapping still wins).
		{"github.com", []string{"github.com"}, "github"},
	}
	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			if got := autodetect.ProviderFor(c.host, c.hosts); got != c.want {
				t.Errorf("ProviderFor(%q): got %q, want %q", c.host, got, c.want)
			}
		})
	}
}
