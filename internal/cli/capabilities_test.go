package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// registerCapFake registers a throwaway provider that supports
// everything except wikis, for the capability-gate tests.
func registerCapFake(t *testing.T, name string) {
	t.Helper()
	provider.Register(provider.Registration{
		Name:        name,
		Unsupported: []provider.Capability{provider.CapWikis},
		Factory:     func(provider.BuildConfig) (provider.Provider, error) { return nil, nil },
	})
}

func TestCapabilityGuardBlocksUnsupported(t *testing.T) {
	clearGaiaEnv(t)
	registerCapFake(t, "capfake-block")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "capfake-block",
		"--api-url", "https://example.test",
		"wiki", "list",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("wiki list against a wiki-less provider: want error, got nil")
	}
	if !strings.Contains(err.Error(), "does not support wikis") {
		t.Fatalf("error = %q, want capability-gate message", err.Error())
	}
}

func TestCapabilityGuardAllowsSupported(t *testing.T) {
	clearGaiaEnv(t)
	registerCapFake(t, "capfake-allow")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	// A capability the provider DOES support (milestones) must not be
	// blocked by the guard — it proceeds and fails later for an
	// unrelated reason (no real backend), never the capability message.
	root.SetArgs([]string{
		"--provider", "capfake-allow",
		"--api-url", "https://example.test",
		"milestone", "list",
	})

	err := root.Execute()
	if err != nil && strings.Contains(err.Error(), "does not support") {
		t.Fatalf("milestone list wrongly gated: %v", err)
	}
}

func TestCapabilityGuardIgnoresMetaCommands(t *testing.T) {
	clearGaiaEnv(t)
	// version has no capability annotation and must run with no provider
	// configured at all — the guard must not touch settings for it.
	var stdout bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("version under capability guard: %v", err)
	}
	if !strings.Contains(stdout.String(), "go_version") {
		t.Fatalf("version output = %q, want version envelope", stdout.String())
	}
}
