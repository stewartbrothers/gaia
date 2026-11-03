package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// aikestraCall is one request the fake Aikestra server saw.
type aikestraCall struct {
	Method, Path, Query string
	Body                map[string]any
}

// runAikestra runs gaia against a fake Aikestra API that answers every
// request with respond(path) and records what it was sent.
func runAikestra(t *testing.T, respond func(path string) string, args ...string) (string, []aikestraCall, error) {
	t.Helper()
	clearGaiaEnv(t)
	t.Setenv("AIKESTRA_TOKEN", "tok")
	var calls []aikestraCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		c := aikestraCall{Method: r.Method, Path: strings.TrimPrefix(r.URL.Path, "/api/v1"), Query: r.URL.RawQuery}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			if err := json.Unmarshal(raw, &c.Body); err != nil {
				t.Errorf("body not JSON: %s", raw)
			}
		}
		calls = append(calls, c)
		_, _ = w.Write([]byte(respond(c.Path)))
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"--provider", "aikestra", "--api-url", srv.URL + "/api/v1", "--repo", "acme/web"}, args...))
	err := root.Execute()
	return stdout.String(), calls, err
}

const aikestraIssue = `{"id":"x","number":2,"title":"Write docs","body":"","type":"docs","state":"open","priority":true,` +
	`"assignee":null,"author":"ada@example.com","parent":null,"repo":null,"board":"Main","stage":"Dev",` +
	`"stage_entered_at":"2026-10-30T00:00:00Z","created_at":"2026-10-30T00:00:00Z","updated_at":"2026-10-30T00:00:00Z","closed_at":null}`

func issueReply(string) string { return aikestraIssue }

func TestAikestraIssueMove(t *testing.T) {
	out, calls, err := runAikestra(t, issueReply, "issue", "move", "2", "--stage", "dev", "--board", "main", "--note", "Starting")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Method != "PUT" || calls[0].Path != "/projects/acme/issues/2/placement" {
		t.Fatalf("calls %+v", calls)
	}
	if b := calls[0].Body; b["stage"] != "dev" || b["board"] != "main" || b["note"] != "Starting" || len(b) != 3 {
		t.Errorf("body %v", b)
	}
	if !strings.Contains(out, `"stage": "Dev"`) {
		t.Errorf("output %s", out)
	}
}

func TestAikestraIssueMoveNeedsStage(t *testing.T) {
	_, calls, err := runAikestra(t, issueReply, "issue", "move", "2")
	if exitcode.Of(err) != exitcode.Usage || len(calls) != 0 {
		t.Fatalf("err %v calls %+v", err, calls)
	}
}

func TestAikestraIssuePriority(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want bool
	}{{"do-now", true}, {"normal", false}} {
		t.Run(tc.arg, func(t *testing.T) {
			_, calls, err := runAikestra(t, issueReply, "issue", "priority", "2", tc.arg)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0].Path != "/projects/acme/issues/2/priority" || calls[0].Body["priority"] != tc.want {
				t.Fatalf("calls %+v", calls)
			}
		})
	}
	_, _, err := runAikestra(t, issueReply, "issue", "priority", "2", "urgent")
	if exitcode.Of(err) != exitcode.Usage {
		t.Fatalf("bad level: %v", err)
	}
}

func TestAikestraIssueCloseWithNote(t *testing.T) {
	_, calls, err := runAikestra(t, issueReply, "issue", "close", "2", "--note", "Done")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Path != "/projects/acme/issues/2/state" || calls[0].Body["note"] != "Done" || calls[0].Body["state"] != "closed" {
		t.Fatalf("calls %+v", calls)
	}
}

func TestAikestraIssueEditAssigneeWithNote(t *testing.T) {
	_, calls, err := runAikestra(t, issueReply, "issue", "edit", "2", "--assignee", "bob@example.com", "--note", "Handing over")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Path != "/projects/acme/issues/2/assignee" || calls[0].Body["note"] != "Handing over" {
		t.Fatalf("calls %+v", calls)
	}
}

func TestAikestraBoards(t *testing.T) {
	board := `{"id":"b","name":"Main","default":true,"paused":false,"created_at":"2026-10-30T00:00:00Z",` +
		`"stages":[{"id":"s","name":"Todo","position":0,"paused":false,"issues":[1,2]}]}`
	out, calls, err := runAikestra(t, func(string) string { return `{"items":[` + board + `],"next_cursor":null}` }, "board", "list")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Path != "/projects/acme/boards" || !strings.Contains(out, `"Main"`) {
		t.Fatalf("calls %+v out %s", calls, out)
	}

	out, calls, err = runAikestra(t, func(string) string { return board }, "board", "view", "main", "--format", "pretty")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Path != "/projects/acme/boards/main" || !strings.Contains(out, "Todo") || !strings.Contains(out, "#1") {
		t.Fatalf("calls %+v out %s", calls, out)
	}
}

func TestAikestraBranchListIsNotGatedByBranchProtection(t *testing.T) {
	refs := `{"items":[{"name":"main","kind":"branch","commit":"a","subject":"s","date":"2026-10-01T00:00:00Z","default":true}],"next_cursor":null,"truncated":false}`
	out, calls, err := runAikestra(t, func(string) string { return refs }, "branch", "list")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Path != "/projects/acme/repos/web/refs" || !strings.Contains(out, `"main"`) {
		t.Fatalf("calls %+v out %s", calls, out)
	}

	_, calls, err = runAikestra(t, issueReply, "branch", "protection", "get", "main")
	if err == nil || !strings.Contains(err.Error(), "does not support branch_protection") || len(calls) != 0 {
		t.Fatalf("protection: err %v calls %+v", err, calls)
	}
}

func TestAikestraGatesUnsupported(t *testing.T) {
	for _, args := range [][]string{{"label", "list"}, {"pr", "list"}, {"milestone", "list"}} {
		_, calls, err := runAikestra(t, issueReply, args...)
		if err == nil || !strings.Contains(err.Error(), "does not support") || len(calls) != 0 {
			t.Errorf("%v: err %v calls %+v", args, err, calls)
		}
	}
}

// search runs the Project's Issue title search (#404).
func TestAikestraSearch(t *testing.T) {
	reply := func(string) string {
		return `{"items":[{"number":12,"title":"Website contact form","repo":null}],"next_cursor":null}`
	}
	out, calls, err := runAikestra(t, reply, "search", "website")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Path != "/projects/acme/issues" || !strings.Contains(calls[0].Query, "q=website") {
		t.Fatalf("calls %+v", calls)
	}
	if !strings.Contains(out, `"number": 12`) {
		t.Errorf("out %s", out)
	}
}

func TestBoardsGatedOnForgejo(t *testing.T) {
	clearGaiaEnv(t)
	root := cli.NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--provider", "forgejo", "--api-url", "https://example.test/api/v1", "--repo", "a/b", "board", "list"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "does not support boards") {
		t.Fatalf("err %v", err)
	}
}

// --type reaches Aikestra's type field on list, create, and edit; a
// label is refused with NotImplemented before any request (briefing v1:
// labels are not-implemented and never emulated).
func TestAikestraIssueType(t *testing.T) {
	list := func(string) string { return `{"items":[` + aikestraIssue + `],"next_cursor":null}` }
	_, calls, err := runAikestra(t, list, "issue", "list", "--type", "bug")
	if err != nil || len(calls) != 1 || !strings.Contains(calls[0].Query, "type=bug") {
		t.Fatalf("list: err %v calls %+v", err, calls)
	}

	_, calls, err = runAikestra(t, issueReply, "issue", "create", "--title", "T", "--type", "bug")
	if err != nil || len(calls) != 1 || calls[0].Method != "POST" || calls[0].Body["type"] != "bug" {
		t.Fatalf("create: err %v calls %+v", err, calls)
	}

	_, calls, err = runAikestra(t, issueReply, "issue", "edit", "2", "--type", "docs")
	if err != nil || len(calls) != 1 || calls[0].Method != "PATCH" || calls[0].Body["type"] != "docs" {
		t.Fatalf("edit: err %v calls %+v", err, calls)
	}

	for _, args := range [][]string{
		{"issue", "list", "--label", "bug"},
		{"issue", "create", "--title", "T", "--label", "bug"},
		{"issue", "edit", "2", "--add-label", "bug"},
	} {
		_, calls, err := runAikestra(t, issueReply, args...)
		if exitcode.Of(err) != exitcode.NotImplemented || len(calls) != 0 {
			t.Errorf("%v: err %v calls %+v", args, err, calls)
		}
	}
}
