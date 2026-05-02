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
)

// jsonRPCCall posts a JSON-RPC 2.0 request to the streamable-HTTP
// endpoint and returns the parsed JSON-RPC envelope. Mirrors what an
// MCP-aware HTTP client (Claude Desktop, Cursor, custom agents) does
// against the production transport.
func jsonRPCCall(t *testing.T, baseURL, sessionID string, body any) (map[string]any, http.Header) {
	t.Helper()
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, raw)
	}
	if len(raw) == 0 {
		// notifications/initialized returns 202 with empty body — caller
		// passes nil-bodied entries through.
		return nil, resp.Header
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode JSON-RPC: %v\nbody: %s", err, raw)
	}
	return out, resp.Header
}

// TestHTTPTransportInitializeListAndCall walks the full client
// handshake against a real httptest server backed by the production
// MCP server (same buildServer() used by `gaia-mcp --http`). Catches
// any regression in the transport-server-tools wiring at one go.
func TestHTTPTransportInitializeListAndCall(t *testing.T) {
	mcpServer := buildServer()
	srv := server.NewTestStreamableHTTPServer(mcpServer)
	defer srv.Close()

	// 1. initialize — returns the server's protocolVersion +
	//    capabilities and gives us a session ID.
	initBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "gaia-mcp-test", "version": "0"},
		},
	}
	initResp, hdrs := jsonRPCCall(t, srv.URL, "", initBody)
	sessionID := hdrs.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatalf("expected Mcp-Session-Id header on initialize response; got headers %+v", hdrs)
	}
	result := initResp["result"].(map[string]any)
	if !strings.HasPrefix(result["protocolVersion"].(string), "2025-") {
		t.Errorf("protocolVersion: %+v", result["protocolVersion"])
	}
	srvInfo := result["serverInfo"].(map[string]any)
	if srvInfo["name"] != "gaia-mcp" {
		t.Errorf("serverInfo.name: %+v", srvInfo)
	}

	// 2. notifications/initialized — required by the protocol after
	//    initialize. No response body expected (202 Accepted).
	jsonRPCCall(t, srv.URL, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})

	// 3. tools/list — returns every registered tool. The smoke set
	//    (gaia_version + gaia_whoami) plus issue/PR/label/search/release
	//    families.
	listResp, _ := jsonRPCCall(t, srv.URL, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{},
	})
	tools := listResp["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 20 {
		t.Errorf("expected ≥20 tools registered; got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, must := range []string{
		"gaia_version", "gaia_whoami",
		"gaia_issue_list", "gaia_pr_list", "gaia_label_list",
		"gaia_search", "gaia_release_list",
	} {
		if !names[must] {
			t.Errorf("missing tool %q in tools/list", must)
		}
	}

	// 4. tools/call gaia_version — exercises the full request →
	//    handler → response path. Doesn't need a forge connection so
	//    no test-builder swap is required.
	callResp, _ := jsonRPCCall(t, srv.URL, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "gaia_version", "arguments": map[string]any{}},
	})
	cr := callResp["result"].(map[string]any)
	content := cr["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"version":`) || !strings.Contains(text, `"go_version":`) {
		t.Errorf("gaia_version text: %q", text)
	}
}

// TestHTTPTransportRejectsCallWithoutSession verifies the
// streamable-HTTP server's session enforcement: a tools/call without
// a Mcp-Session-Id from a prior initialize must fail. (mcp-go's
// stateful session manager rejects orphan calls; this pins that
// behavior so we'd notice if the default switched to stateless.)
func TestHTTPTransportRejectsCallWithoutSession(t *testing.T) {
	srv := server.NewTestStreamableHTTPServer(buildServer())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 99, "method": "tools/call",
		"params": map[string]any{"name": "gaia_version", "arguments": map[string]any{}},
	})
	req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 4xx for sessionless tools/call; got %d: %s", resp.StatusCode, raw)
	}
}
