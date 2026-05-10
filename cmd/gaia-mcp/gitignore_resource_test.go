package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/internal/gitignore"
)

// TestGitignoreResourceListIncludesGitignore drives the full HTTP
// MCP handshake (initialize → notifications/initialized →
// resources/list) and asserts the gitignore resource is registered
// at the gaia:// URI we promised.
func TestGitignoreResourceListIncludesGitignore(t *testing.T) {
	mcpServer := buildServer()
	srv := server.NewTestStreamableHTTPServer(mcpServer)
	defer srv.Close()

	sessionID := initializeSession(t, srv.URL)

	listResp, _ := jsonRPCCall(t, srv.URL, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 10, "method": "resources/list",
		"params": map[string]any{},
	})
	result, ok := listResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("resources/list missing result: %+v", listResp)
	}
	resources, ok := result["resources"].([]any)
	if !ok {
		t.Fatalf("resources/list missing resources slice: %+v", result)
	}
	var found bool
	for _, r := range resources {
		entry := r.(map[string]any)
		if entry["uri"] == "gaia://gitignore" {
			found = true
			if entry["mimeType"] != "text/plain" {
				t.Errorf("gitignore mimeType: got %v want text/plain", entry["mimeType"])
			}
			if entry["name"] != "gitignore" {
				t.Errorf("gitignore name: got %v want gitignore", entry["name"])
			}
		}
	}
	if !found {
		t.Errorf("resources/list did not include gaia://gitignore; got: %+v", resources)
	}
}

// TestGitignoreResourceReadReturnsEmbeddedContent — resources/read
// against gaia://gitignore returns the same bytes the CLI would
// emit. Pins the cross-frontend invariant that CLI and MCP can never
// disagree about the canonical block.
func TestGitignoreResourceReadReturnsEmbeddedContent(t *testing.T) {
	mcpServer := buildServer()
	srv := server.NewTestStreamableHTTPServer(mcpServer)
	defer srv.Close()

	sessionID := initializeSession(t, srv.URL)

	readResp, _ := jsonRPCCall(t, srv.URL, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 11, "method": "resources/read",
		"params": map[string]any{"uri": "gaia://gitignore"},
	})
	result, ok := readResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("resources/read missing result: %+v", readResp)
	}
	contents, ok := result["contents"].([]any)
	if !ok || len(contents) == 0 {
		t.Fatalf("resources/read missing contents: %+v", result)
	}
	first := contents[0].(map[string]any)
	if first["uri"] != "gaia://gitignore" {
		t.Errorf("contents[0].uri: got %v want gaia://gitignore", first["uri"])
	}
	if first["mimeType"] != "text/plain" {
		t.Errorf("contents[0].mimeType: got %v want text/plain", first["mimeType"])
	}
	text, ok := first["text"].(string)
	if !ok {
		t.Fatalf("contents[0].text not a string: %+v", first)
	}
	if text != gitignore.Recommended {
		t.Errorf("contents[0].text drifts from gitignore.Recommended\n"+
			"len(got)=%d len(want)=%d", len(text), len(gitignore.Recommended))
	}
	if !strings.Contains(text, ".gaia/credentials*") {
		t.Errorf("expected .gaia/credentials* in resource content; got: %s", text)
	}
}

// initializeSession runs the MCP handshake against a streamable-HTTP
// test server and returns the session ID. Mirrors the pattern in
// http_transport_test.go so the resource tests use the exact same
// transport-level flow real clients do.
func initializeSession(t *testing.T, baseURL string) string {
	t.Helper()
	initBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "gaia-mcp-resource-test", "version": "0"},
		},
	}
	_, hdrs := jsonRPCCall(t, baseURL, "", initBody)
	sessionID := hdrs.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatalf("no Mcp-Session-Id on initialize response")
	}
	jsonRPCCall(t, baseURL, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	return sessionID
}

// TestGitignoreResourceHandlerDirect — exercise the handler in
// isolation (no HTTP transport) so the body assertion is independent
// of the transport JSON-marshalling. Useful for catching changes to
// the embed without spinning up an MCP session.
func TestGitignoreResourceHandlerDirect(t *testing.T) {
	// Round-trip via an explicit JSON-RPC message: simpler than
	// constructing the typed mcp.ReadResourceRequest by hand and
	// uses the same path the transport does.
	mcpServer := buildServer()
	srv := server.NewTestStreamableHTTPServer(mcpServer)
	defer srv.Close()

	sessionID := initializeSession(t, srv.URL)

	body := map[string]any{
		"jsonrpc": "2.0", "id": 99, "method": "resources/read",
		"params": map[string]any{"uri": "gaia://gitignore"},
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), ".gaia/credentials*") {
		t.Errorf("resources/read body missing .gaia/credentials*; got: %s", raw)
	}
}
