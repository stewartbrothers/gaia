package provider_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

// stubProvider is a no-op Provider used purely to give a Factory
// something to return; it embeds the wide interface so we don't restate
// 50 methods (the methods panic if ever called, which they aren't here).
type stubProvider struct{ provider.Provider }

func TestRegisterBuildAndRegistered(t *testing.T) {
	name := "test-reg-build"
	provider.Register(provider.Registration{
		Name:          name,
		DefaultAPIURL: "https://example.test",
		TokenEnvNames: []string{"TEST_TOKEN", "ALT_TOKEN"},
		Factory: func(cfg provider.BuildConfig) (provider.Provider, error) {
			if cfg.APIURL == "" {
				return nil, fmt.Errorf("APIURL required")
			}
			return &stubProvider{}, nil
		},
	})

	// Lookup + metadata.
	reg, ok := provider.Lookup(name)
	if !ok || reg.DefaultAPIURL != "https://example.test" {
		t.Fatalf("Lookup(%q): ok=%v reg=%+v", name, ok, reg)
	}
	if got := provider.TokenEnvNames(name); len(got) != 2 || got[0] != "TEST_TOKEN" {
		t.Fatalf("TokenEnvNames: got %v", got)
	}

	// Build happy path.
	p, err := provider.Build(name, provider.BuildConfig{APIURL: "https://x"})
	if err != nil || p == nil {
		t.Fatalf("Build: p=%v err=%v", p, err)
	}

	// Factory error propagates verbatim.
	if _, err := provider.Build(name, provider.BuildConfig{}); err == nil {
		t.Fatal("Build with empty APIURL: want factory error, got nil")
	}

	// Registered includes our name.
	found := false
	for _, n := range provider.Registered() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("Registered() %v missing %q", provider.Registered(), name)
	}
}

func TestBuildUnknownProviderIsTyped(t *testing.T) {
	_, err := provider.Build("definitely-not-registered", provider.BuildConfig{})
	if !errors.Is(err, provider.ErrUnknownProvider) {
		t.Fatalf("Build unknown: want ErrUnknownProvider, got %v", err)
	}
}

func TestTokenEnvNamesUnknownIsNil(t *testing.T) {
	if got := provider.TokenEnvNames("nope-not-here"); got != nil {
		t.Fatalf("TokenEnvNames(unknown): want nil, got %v", got)
	}
}

func TestRegisterPanics(t *testing.T) {
	cases := map[string]provider.Registration{
		"empty name":  {Name: "", Factory: func(provider.BuildConfig) (provider.Provider, error) { return nil, nil }},
		"nil factory": {Name: "has-name-no-factory"},
	}
	for desc, reg := range cases {
		t.Run(desc, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("Register(%s): expected panic", desc)
				}
			}()
			provider.Register(reg)
		})
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	name := "test-reg-dup"
	f := func(provider.BuildConfig) (provider.Provider, error) { return &stubProvider{}, nil }
	provider.Register(provider.Registration{Name: name, Factory: f})
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register: expected panic")
		}
	}()
	provider.Register(provider.Registration{Name: name, Factory: f})
}

// TestRegistryConcurrencySafe runs Register / Lookup / Build / Registered
// concurrently under -race to prove the mutex guards every path.
func TestRegistryConcurrencySafe(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("conc-%d", i)
			provider.Register(provider.Registration{
				Name:    name,
				Factory: func(provider.BuildConfig) (provider.Provider, error) { return &stubProvider{}, nil },
			})
			_, _ = provider.Lookup(name)
			_, _ = provider.Build(name, provider.BuildConfig{})
			_ = provider.Registered()
			_ = provider.TokenEnvNames(name)
		}(i)
	}
	wg.Wait()
}
