package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/stewartbrothers/gaia/core/forgejo"
)

// fakeForgeProvider builds a *forgejo.Provider against an httptest
// server. Used by every MCP tool test that needs the forge online.
func fakeForgeProvider(t *testing.T, h http.HandlerFunc) (*forgejo.Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := forgejo.NewProvider(forgejo.Options{
		BaseURL:   srv.URL,
		Token:     "TEST",
		RetryWait: 1 * time.Millisecond,
	})
	return p, srv
}

// pinBuilder swaps the MCP builder to return p, and restores on test
// cleanup.
func pinBuilder(t *testing.T, p *forgejo.Provider) {
	t.Helper()
	SetBuilderForTest(func() (*forgejo.Provider, error) { return p, nil })
	t.Cleanup(func() { SetBuilderForTest(nil) })
}

// callTool invokes a tool handler with the given arguments and
// returns the result. The handler is expected to be a closure that
// takes a CallToolRequest; we synthesise that here.
func callTool(ctx context.Context, fn func(context.Context, map[string]any) (*mcp.CallToolResult, error), args map[string]any) (*mcp.CallToolResult, error) {
	return fn(ctx, args)
}

// envelopeData decodes the JSON text out of an MCP tool result and
// returns the envelope.data subtree. Fails the test on any decode or
// shape error.
func envelopeData(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("nil or empty tool result")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent; got %T", res.Content[0])
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatalf("decode envelope: %v\ntext: %s", err, tc.Text)
	}
	if env["schema_version"] != "1.0" {
		t.Errorf("schema_version: got %v", env["schema_version"])
	}
	d, ok := env["data"].(map[string]any)
	if !ok {
		// Sometimes data is an array or scalar; cast in the caller's test.
		return map[string]any{"_raw_data": env["data"]}
	}
	return d
}

// envelopeSlice is the array variant of envelopeData.
func envelopeSlice(t *testing.T, res *mcp.CallToolResult) []any {
	t.Helper()
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent; got %T", res.Content[0])
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatal(err)
	}
	arr, ok := env["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array; got %T", env["data"])
	}
	return arr
}

// resultText returns the raw text content of a tool result. Used for
// error-path assertions.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestHandleVersion(t *testing.T) {
	res, err := handleVersion(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleVersion: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, `"version":`) || !strings.Contains(text, `"go_version":`) {
		t.Errorf("expected version+go_version in text; got %q", text)
	}
}

func TestHandleWhoami(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "Gerwood"})
	})
	pinBuilder(t, p)

	res, err := handleWhoami(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}
	if !strings.Contains(resultText(t, res), `"login":"Gerwood"`) {
		t.Errorf("expected Gerwood in result; got %q", resultText(t, res))
	}
}

func TestHandleWhoamiAuthError(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	})
	pinBuilder(t, p)

	res, err := handleWhoami(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleWhoami should return tool error, not transport error; got %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true; got %+v", res)
	}
}
