package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/envelope"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/settings"
	"github.com/stewartbrothers/gaia/internal/forgebuilder"
)

// toolResult wraps data in the standard envelope and JSON-encodes for
// the MCP response. Mirrors the CLI's renderEnvelope so MCP tools and
// `gaia <verb> --format json` produce the same on-the-wire shape.
func toolResult(data any, page *provider.Page) *mcp.CallToolResult {
	env := envelope.New(data).WithPage(page)
	b, err := json.Marshal(env)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("encode envelope: %v", err))
	}
	return mcp.NewToolResultText(string(b))
}

// toolError surfaces err as an MCP tool error. The exit-code wrapping
// is preserved in the underlying error so consumers that care can
// inspect via errors.As.
func toolError(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}

// repoFromArgs parses the "repo" argument (owner/name) supplied by
// the MCP client. Required for every repo-scoped tool.
func repoFromArgs(args map[string]any) (owner, repo string, err error) {
	r, _ := args["repo"].(string)
	r = strings.TrimSpace(r)
	if r == "" {
		return "", "", exitcode.Errorf(exitcode.Usage, "repo is required (owner/name)")
	}
	parts := strings.SplitN(r, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", exitcode.Errorf(exitcode.Usage, "repo must be owner/name; got %q", r)
	}
	return parts[0], parts[1], nil
}

// build resolves config + credentials into a Provider. Each tool
// calls this; failures map to an MCP tool error. Returns the
// provider.Provider interface so the underlying value can be either
// *forgejo.Provider or *github.Provider depending on what
// forgebuilder dispatches to.
//
// Takes ctx so the HTTP transport can plumb a per-request bearer
// (the client's own forge PAT) through forgeTokenFromContext into
// forgebuilder.Override.Token. In stdio mode the context never
// carries a token and forgebuilder falls back to the layered
// credential store.
//
// Indirection via builderFn lets tests swap in a fake provider; the
// production path goes through forgebuilder.
func build(ctx context.Context) (provider.Provider, error) {
	return builderFn(ctx)
}

// builderFn is the swappable provider builder. Tests change this via
// SetBuilderForTest in export_test.go; production stays on
// defaultBuilder.
var builderFn = defaultBuilder

// serverSettingsOnce ensures settings.Load runs exactly once per
// gaia-mcp process — every tool call afterwards uses the cached
// handle and only the per-request bearer token differs. Config,
// credentials, and env are not re-read per request. (#311)
var (
	serverSettingsOnce sync.Once
	serverSettings     settings.Settings
	serverSettingsErr  error
)

func defaultBuilder(ctx context.Context) (provider.Provider, error) {
	serverSettingsOnce.Do(func() {
		serverSettings, serverSettingsErr = settings.Load(settings.Override{})
	})
	if serverSettingsErr != nil {
		return nil, serverSettingsErr
	}
	p, _, err := forgebuilder.Build(serverSettings, forgebuilder.BuildOverride{
		Token: forgeTokenFromContext(ctx),
	})
	return p, err
}

// optInt extracts an integer argument; mcp-go decodes JSON numbers as
// float64, so we coerce. Missing or zero returns 0 — caller decides
// whether that's allowed.
func optInt(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// optString returns args[key] as a string, "" when absent.
func optString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

// optStringSlice returns args[key] as []string. mcp-go gives us
// []any from JSON arrays; coerce each element.
func optStringSlice(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// optBool returns args[key] as a bool, false when absent.
func optBool(args map[string]any, key string) bool {
	b, _ := args[key].(bool)
	return b
}

// registerAllTools wires every gaia operation as an MCP tool.
// Grouped per resource so it's easy to scan: read tools first, write
// tools second within each section.
func registerAllTools(s *server.MCPServer) {
	registerIssueTools(s)
	registerPRTools(s)
	registerLabelTools(s)
	registerSearchTool(s)
	registerReleaseTools(s)
	registerPackagesTools(s)
	registerWikiTools(s)
	registerWebhookTools(s)
	registerActionsTools(s)
	registerMilestoneTools(s)
}

// ctxBoundHandler is a tiny helper that runs an MCP tool handler
// using the request's context. Tools can return an error which becomes
// an MCP tool error rather than a transport error.
func ctxBoundHandler(fn func(context.Context, map[string]any) (*mcp.CallToolResult, error)) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		return fn(ctx, args)
	}
}
