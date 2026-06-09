package main

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/provider"
)

func hasToolWithPrefix(s *server.MCPServer, prefix string) bool {
	for name := range s.ListTools() {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// TestRegisterToolsOmitsUnsupported pins #342: a provider that declares a
// capability unsupported gets its tool group left out of the manifest,
// while supported groups remain.
func TestRegisterToolsOmitsUnsupported(t *testing.T) {
	provider.Register(provider.Registration{
		Name:        "capfake-mcp-nowiki",
		Unsupported: []provider.Capability{provider.CapWikis},
		Factory:     func(provider.BuildConfig) (provider.Provider, error) { return nil, nil },
	})

	s := server.NewMCPServer("gaia-mcp-test", "test")
	registerToolsForProvider(s, "capfake-mcp-nowiki")

	if hasToolWithPrefix(s, "gaia_wiki") {
		t.Error("wiki tools registered for a provider that declares wikis unsupported")
	}
	// Supported groups and the always-on groups must still be present.
	for _, prefix := range []string{"gaia_pr", "gaia_release", "gaia_issue", "gaia_milestone"} {
		if !hasToolWithPrefix(s, prefix) {
			t.Errorf("expected %q tools to be registered", prefix)
		}
	}
}

// TestRegisterToolsPermissiveDefault pins backwards-compat: an
// unregistered / fully-supported provider name registers every group,
// including wikis.
func TestRegisterToolsPermissiveDefault(t *testing.T) {
	s := server.NewMCPServer("gaia-mcp-test", "test")
	registerToolsForProvider(s, "some-unregistered-name")

	for _, prefix := range []string{"gaia_wiki", "gaia_pr", "gaia_release", "gaia_webhook", "gaia_actions", "gaia_packages", "gaia_milestone"} {
		if !hasToolWithPrefix(s, prefix) {
			t.Errorf("permissive default: expected %q tools registered", prefix)
		}
	}
}
