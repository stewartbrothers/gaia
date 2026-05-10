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

	agentguide "github.com/stewartbrothers/gaia"
)

// TestLearnResourceListIncludesLearn drives the full HTTP MCP
// handshake (initialize → notifications/initialized →
// resources/list) and asserts the learn resource is registered at
// the gaia:// URI we promised. Mirrors the pattern PR #273
// established for the gitignore resource — same handshake shape, so
// the resource list is exercised through the production transport
// the way real clients hit it.
func TestLearnResourceListIncludesLearn(t *testing.T) {
	mcpServer := buildServer()
	srv := server.NewTestStreamableHTTPServer(mcpServer)
	defer srv.Close()

	sessionID := initializeLearnSession(t, srv.URL)

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
		if entry["uri"] == "gaia://learn" {
			found = true
			if entry["mimeType"] != "text/markdown" {
				t.Errorf("learn mimeType: got %v want text/markdown", entry["mimeType"])
			}
			if entry["name"] != "learn" {
				t.Errorf("learn name: got %v want learn", entry["name"])
			}
		}
	}
	if !found {
		t.Errorf("resources/list did not include gaia://learn; got: %+v", resources)
	}
}

// TestLearnResourceReadReturnsEmbeddedContent — resources/read
// against gaia://learn returns the same bytes the CLI would emit
// for `gaia learn`. Pins the cross-frontend invariant that CLI and
// MCP can never disagree about the agent guide content.
func TestLearnResourceReadReturnsEmbeddedContent(t *testing.T) {
	mcpServer := buildServer()
	srv := server.NewTestStreamableHTTPServer(mcpServer)
	defer srv.Close()

	sessionID := initializeLearnSession(t, srv.URL)

	readResp, _ := jsonRPCCall(t, srv.URL, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 11, "method": "resources/read",
		"params": map[string]any{"uri": "gaia://learn"},
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
	if first["uri"] != "gaia://learn" {
		t.Errorf("contents[0].uri: got %v want gaia://learn", first["uri"])
	}
	if first["mimeType"] != "text/markdown" {
		t.Errorf("contents[0].mimeType: got %v want text/markdown", first["mimeType"])
	}
	text, ok := first["text"].(string)
	if !ok {
		t.Fatalf("contents[0].text not a string: %+v", first)
	}
	if text != agentguide.Markdown {
		t.Errorf("contents[0].text drifts from agentguide.Markdown\n"+
			"len(got)=%d len(want)=%d", len(text), len(agentguide.Markdown))
	}
	if len(text) == 0 {
		t.Errorf("expected non-empty agent-guide markdown; got empty string")
	}
}

// TestLearnResourceHandlerDirect — exercise the resource handler
// over an explicit JSON-RPC POST so the body assertion is
// independent of any typed-request marshalling. Useful for catching
// changes to the embed without spinning up the full mcp-go client.
func TestLearnResourceHandlerDirect(t *testing.T) {
	mcpServer := buildServer()
	srv := server.NewTestStreamableHTTPServer(mcpServer)
	defer srv.Close()

	sessionID := initializeLearnSession(t, srv.URL)

	body := map[string]any{
		"jsonrpc": "2.0", "id": 99, "method": "resources/read",
		"params": map[string]any{"uri": "gaia://learn"},
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
	// The agent guide markdown reliably contains the literal "gaia"
	// token (it's the project name and appears throughout the doc).
	// A non-empty embed without that string would mean the wrong
	// file was wired up.
	if !strings.Contains(string(raw), "gaia") {
		t.Errorf("resources/read body missing 'gaia' token; got: %s", raw)
	}
}

// initializeLearnSession runs the MCP handshake against a
// streamable-HTTP test server and returns the session ID. Local to
// this file (uniquely named) so it can coexist with the gitignore
// resource tests' equivalent helper without symbol collision while
// PR #273 is pending — and afterwards too. Mirrors the handshake in
// http_transport_test.go so the resource tests use the exact same
// transport-level flow real clients do.
func initializeLearnSession(t *testing.T, baseURL string) string {
	t.Helper()
	initBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "gaia-mcp-learn-test", "version": "0"},
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
