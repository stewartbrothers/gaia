package provider_test

import (
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

func TestSupportsDefaultsPermissive(t *testing.T) {
	// A forge that declares no Unsupported capabilities supports
	// everything — the Forgejo/GitHub case.
	name := "cap-allsupported"
	provider.Register(provider.Registration{
		Name:    name,
		Factory: func(provider.BuildConfig) (provider.Provider, error) { return &stubProvider{}, nil },
	})
	for _, c := range []provider.Capability{
		provider.CapPullRequests, provider.CapWikis, provider.CapReleases,
		provider.CapWebhooks, provider.CapPackages, provider.CapActions, provider.CapMilestones,
	} {
		if !provider.Supports(name, c) {
			t.Errorf("Supports(%q, %q) = false, want true (empty Unsupported)", name, c)
		}
	}
}

func TestSupportsUnknownProviderIsPermissive(t *testing.T) {
	if !provider.Supports("no-such-forge", provider.CapWikis) {
		t.Fatal("Supports(unknown) = false, want true")
	}
	if got := provider.UnsupportedCapabilities("no-such-forge"); got != nil {
		t.Fatalf("UnsupportedCapabilities(unknown) = %v, want nil", got)
	}
}

func TestSupportsHonoursUnsupported(t *testing.T) {
	name := "cap-issues-only"
	provider.Register(provider.Registration{
		Name:        name,
		Unsupported: []provider.Capability{provider.CapPullRequests, provider.CapWikis, provider.CapReleases},
		Factory:     func(provider.BuildConfig) (provider.Provider, error) { return &stubProvider{}, nil },
	})

	// Declared-unsupported categories are hidden.
	for _, c := range []provider.Capability{provider.CapPullRequests, provider.CapWikis, provider.CapReleases} {
		if provider.Supports(name, c) {
			t.Errorf("Supports(%q, %q) = true, want false", name, c)
		}
	}
	// Everything else is still supported.
	for _, c := range []provider.Capability{provider.CapWebhooks, provider.CapPackages, provider.CapActions, provider.CapMilestones} {
		if !provider.Supports(name, c) {
			t.Errorf("Supports(%q, %q) = false, want true", name, c)
		}
	}
	if got := provider.UnsupportedCapabilities(name); len(got) != 3 {
		t.Fatalf("UnsupportedCapabilities = %v, want 3 entries", got)
	}
}
