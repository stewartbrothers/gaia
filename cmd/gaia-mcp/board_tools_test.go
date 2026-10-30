package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/aikestra"
	"github.com/stewartbrothers/gaia/core/provider"
)

// pinAikestra serves h as an Aikestra API and pins the MCP builder to it.
func pinAikestra(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := aikestra.NewProvider(aikestra.Options{BaseURL: srv.URL, Token: "tok"})
	SetBuilderForTest(func(_ context.Context) (provider.Provider, error) { return p, nil })
	t.Cleanup(func() { SetBuilderForTest(nil) })
}

const boardToolIssue = `{"number":2,"title":"t","body":"","type":"docs","state":"open","priority":true,"assignee":null,` +
	`"author":"ada@example.com","parent":null,"board":"Main","stage":"Dev","created_at":"2026-10-30T00:00:00Z",` +
	`"updated_at":"2026-10-30T00:00:00Z","closed_at":null}`

// recordJSON answers every request with reply and captures the last
// request's method, path, and JSON body.
func recordJSON(t *testing.T, reply string, method, path *string, body *map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*method, *path = r.Method, r.URL.Path
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			if err := json.Unmarshal(raw, body); err != nil {
				t.Errorf("body: %s", raw)
			}
		}
		_, _ = w.Write([]byte(reply))
	}
}

func TestIssueMoveTool(t *testing.T) {
	var method, path string
	var body map[string]any
	pinAikestra(t, recordJSON(t, boardToolIssue, &method, &path, &body))

	res, err := callTool(context.Background(), handleIssueMove, map[string]any{
		"repo": "acme/web", "number": float64(2), "stage": "dev", "note": "Starting",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if method != http.MethodPut || path != "/projects/acme/issues/2/placement" || body["stage"] != "dev" || body["note"] != "Starting" {
		t.Errorf("%s %s %v", method, path, body)
	}
	if data := envelopeData(t, res); data["stage"] != "Dev" {
		t.Errorf("stage: %v", data["stage"])
	}
}

func TestIssueMoveToolRequiresStage(t *testing.T) {
	res, _ := callTool(context.Background(), handleIssueMove, map[string]any{"repo": "acme/web", "number": float64(2)})
	if res == nil || !res.IsError {
		t.Fatal("missing stage: expected tool error")
	}
}

func TestIssuePriorityTool(t *testing.T) {
	var method, path string
	var body map[string]any
	pinAikestra(t, recordJSON(t, boardToolIssue, &method, &path, &body))

	res, err := callTool(context.Background(), handleIssuePriority, map[string]any{
		"repo": "acme/web", "number": float64(2), "do_now": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if path != "/projects/acme/issues/2/priority" || body["priority"] != true {
		t.Errorf("%s %v", path, body)
	}
}

func TestBoardTools(t *testing.T) {
	board := `{"name":"Main","default":true,"paused":false,"stages":[{"name":"Todo","paused":false,"issues":[1]}]}`
	var method, path string
	var body map[string]any
	pinAikestra(t, recordJSON(t, board, &method, &path, &body))
	res, err := callTool(context.Background(), handleBoardView, map[string]any{"repo": "acme/web", "board": "main"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if path != "/projects/acme/boards/main" || envelopeData(t, res)["name"] != "Main" {
		t.Errorf("path %s res %s", path, resultText(t, res))
	}

	pinAikestra(t, recordJSON(t, `{"items":[`+board+`],"next_cursor":null}`, &method, &path, &body))
	res, err = callTool(context.Background(), handleBoardList, map[string]any{"repo": "acme/web"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if path != "/projects/acme/boards" {
		t.Errorf("path %s", path)
	}
}

func TestIssueEditToolPassesNote(t *testing.T) {
	var method, path string
	var body map[string]any
	pinAikestra(t, recordJSON(t, boardToolIssue, &method, &path, &body))
	res, err := callTool(context.Background(), handleIssueEdit, map[string]any{
		"repo": "acme/web", "number": float64(2), "state": "closed", "note": "Done",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if path != "/projects/acme/issues/2/state" || body["note"] != "Done" {
		t.Errorf("%s %v", path, body)
	}
}

func TestRegisterToolsForAikestra(t *testing.T) {
	s := server.NewMCPServer("gaia-mcp-test", "test")
	registerToolsForProvider(s, "aikestra")
	tools := s.ListTools()
	for _, name := range []string{"gaia_board_list", "gaia_board_view", "gaia_issue_move", "gaia_issue_priority", "gaia_branch_list", "gaia_tag_list", "gaia_issue_list", "gaia_issue_dep_list"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	for _, prefix := range []string{"gaia_label", "gaia_search", "gaia_pr", "gaia_branch_protection", "gaia_milestone", "gaia_wiki"} {
		if hasToolWithPrefix(s, prefix) {
			t.Errorf("%s tools registered for aikestra", prefix)
		}
	}
}

func TestRegisterToolsHidesBoardsOnForgejo(t *testing.T) {
	s := server.NewMCPServer("gaia-mcp-test", "test")
	registerToolsForProvider(s, "forgejo")
	for _, prefix := range []string{"gaia_board", "gaia_issue_move", "gaia_issue_priority"} {
		if hasToolWithPrefix(s, prefix) {
			t.Errorf("%s registered for forgejo", prefix)
		}
	}
	for _, prefix := range []string{"gaia_branch_protection", "gaia_label", "gaia_search"} {
		if !hasToolWithPrefix(s, prefix) {
			t.Errorf("%s missing for forgejo", prefix)
		}
	}
}
