package aikestra

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func wantCode(t *testing.T, err error, code int, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want exit %d, got nil error", code)
	}
	if got := exitcode.Of(err); got != code {
		t.Errorf("exit code = %d, want %d (%v)", got, code, err)
	}
	if substr != "" && !strings.Contains(err.Error(), substr) {
		t.Errorf("error %q does not mention %q", err, substr)
	}
}

func TestWhoami(t *testing.T) {
	for _, name := range []string{"user", "user_agent"} {
		t.Run(name, func(t *testing.T) {
			got, err := replay(t, name).Whoami(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != "ada@example.com" {
				t.Errorf("Whoami = %q", got)
			}
		})
	}
}

func TestWhoami_Unauthenticated(t *testing.T) {
	_, err := replay(t, "unauthenticated").Whoami(context.Background())
	wantCode(t, err, exitcode.Auth, "unauthenticated")
}

func TestServerVersion(t *testing.T) {
	got, err := replay(t, "openapi").ServerVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.0.0" {
		t.Errorf("Version = %q", got.Version)
	}
}

func TestListIssues_WalksCursorToNull(t *testing.T) {
	p := replay(t, "issues_page1", "issues_page2")
	ctx := context.Background()

	first, page, err := p.ListIssues(ctx, "acme", "web", provider.ListIssuesOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Number != 4 || !page.Truncated || page.NextCursor == "" {
		t.Fatalf("page 1 = %v %+v", numbers(first), page)
	}

	// The last page is exactly full; the null cursor, not the size, ends it.
	second, page, err := p.ListIssues(ctx, "acme", "web", provider.ListIssuesOptions{Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || page.Truncated || page.NextCursor != "" {
		t.Fatalf("page 2 = %v %+v", numbers(second), page)
	}
}

func TestListIssues_Filters(t *testing.T) {
	got, _, err := replay(t, "issues_filtered").ListIssues(context.Background(), "acme", "", provider.ListIssuesOptions{
		State: "all", Labels: []string{"bug"}, Assignee: "@me", Query: "login", Limit: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "bug" {
		t.Fatalf("got %+v", got)
	}
}

func TestListIssues_ClampsLimit(t *testing.T) {
	if clampLimit(500) != 100 || clampLimit(0) != 30 || clampLimit(7) != 7 {
		t.Fatal("clampLimit")
	}
}

func TestListIssues_Refused(t *testing.T) {
	p := replay(t) // no request may be made
	ctx := context.Background()
	_, _, err := p.ListIssues(ctx, "acme", "", provider.ListIssuesOptions{Author: "ada@example.com"})
	wantCode(t, err, exitcode.NotImplemented, "author")
	_, _, err = p.ListIssues(ctx, "acme", "", provider.ListIssuesOptions{Since: time.Now()})
	wantCode(t, err, exitcode.NotImplemented, "")
	_, _, err = p.ListIssues(ctx, "acme", "", provider.ListIssuesOptions{Labels: []string{"bug", "docs"}})
	wantCode(t, err, exitcode.Usage, "one type")
}

func TestGetIssue(t *testing.T) {
	got, err := replay(t, "issue_get").GetIssue(context.Background(), "acme", "web", 1, provider.GetIssueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 1 || got.Title != "Login is slow" || got.Body != "Make it fast" || got.State != "open" {
		t.Errorf("basics: %+v", got)
	}
	if got.Type != "bug" || got.Board != "Main" || got.Stage != "Todo" || got.DoNow {
		t.Errorf("aikestra fields: %+v", got)
	}
	if got.Author.Login != "ada@example.com" || len(got.Assignees) != 1 || got.Assignees[0].Login != "ada@example.com" {
		t.Errorf("people: %+v %+v", got.Author, got.Assignees)
	}
	if got.CreatedAt.IsZero() || got.ClosedAt != nil || len(got.Labels) != 0 {
		t.Errorf("times/labels: %+v", got)
	}
}

func TestGetIssue_Inlines(t *testing.T) {
	p := replay(t, "issue_get", "comments")
	got, err := p.GetIssue(context.Background(), "acme", "", 1, provider.GetIssueOptions{WithComments: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Comments) != 1 || got.Comments[0].Body != "Agreed" {
		t.Errorf("most recent comment: %+v", got.Comments)
	}
}

func TestGetIssue_NotFoundAndNoAccessLookTheSame(t *testing.T) {
	for _, name := range []string{"issue_get_missing", "issue_get_no_access"} {
		t.Run(name, func(t *testing.T) {
			n := 999
			if name == "issue_get_no_access" {
				n = 1
			}
			_, err := replay(t, name).GetIssue(context.Background(), "acme", "", n, provider.GetIssueOptions{})
			wantCode(t, err, exitcode.NotFound, "not visible")
		})
	}
}

func TestCreateIssue(t *testing.T) {
	got, err := replay(t, "issue_create").CreateIssue(context.Background(), "acme", "web", provider.CreateIssueOptions{
		Title: "Login is slow", Body: "Make it fast", Labels: []string{"bug"}, Assignees: []string{"@me"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 1 || got.Type != "bug" {
		t.Errorf("got %+v", got)
	}

	got, err = replay(t, "issue_create_minimal").CreateIssue(context.Background(), "acme", "", provider.CreateIssueOptions{Title: "Write docs"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "task" || len(got.Assignees) != 0 {
		t.Errorf("defaults: %+v", got)
	}
}

func TestCreateIssue_Refused(t *testing.T) {
	p := replay(t)
	ctx := context.Background()
	_, err := p.CreateIssue(ctx, "acme", "", provider.CreateIssueOptions{Title: "x", Milestone: 3})
	wantCode(t, err, exitcode.NotImplemented, "milestones")
	_, err = p.CreateIssue(ctx, "acme", "", provider.CreateIssueOptions{Title: "x", Assignees: []string{"a", "b"}})
	wantCode(t, err, exitcode.Usage, "one assignee")
}

func TestCreateIssue_ValidationFailed(t *testing.T) {
	// The unknown type is the server's call: types are per-Project.
	_, err := replay(t, "validation_failed").CreateIssue(context.Background(), "acme", "", provider.CreateIssueOptions{
		Title: "Typed", Labels: []string{"nonsense"},
	})
	wantCode(t, err, exitcode.Usage, "validation_failed")
}

func TestEditIssue_PatchesTitleAndType(t *testing.T) {
	got, err := replay(t, "issue_patch").EditIssue(context.Background(), "acme", "", 2, provider.EditIssueOptions{
		Title: "Write the docs", AddLabels: []string{"docs"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Write the docs" || got.Type != "docs" {
		t.Errorf("got %+v", got)
	}
}

func TestEditIssue_StateThenAssignee(t *testing.T) {
	got, err := replay(t, "issue_close", "issue_assign").EditIssue(context.Background(), "acme", "", 2, provider.EditIssueOptions{
		State: "closed", Assignees: []string{"bob@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The last write's response is returned.
	if len(got.Assignees) != 1 || got.Assignees[0].Login != "bob@example.com" {
		t.Errorf("got %+v", got)
	}
}

func TestEditIssue_Reopen(t *testing.T) {
	got, err := replay(t, "issue_reopen").EditIssue(context.Background(), "acme", "", 2, provider.EditIssueOptions{State: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "open" || got.ClosedAt != nil {
		t.Errorf("got %+v", got)
	}
}

func TestEditIssue_NoteRequired(t *testing.T) {
	_, err := replay(t, "note_required").EditIssue(context.Background(), "acme", "", 2, provider.EditIssueOptions{State: "closed"})
	wantCode(t, err, exitcode.Usage, "--note")

	for _, tc := range []struct{ fixture, state, note string }{
		{"issue_close_with_note", "closed", "Done"},
		{"issue_reopen_with_note", "open", "Not done"},
	} {
		got, err := replay(t, tc.fixture).EditIssue(context.Background(), "acme", "", 2, provider.EditIssueOptions{State: tc.state, Note: tc.note})
		if err != nil {
			t.Fatal(err)
		}
		if got.State != tc.state {
			t.Errorf("%s: state %q", tc.fixture, got.State)
		}
	}
}

func TestEditIssue_NothingToChangeReadsIssue(t *testing.T) {
	got, err := replay(t, "issue_get").EditIssue(context.Background(), "acme", "", 1, provider.EditIssueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestEditIssue_RefusedBeforeAnyWrite(t *testing.T) {
	p := replay(t)
	ctx := context.Background()
	// Each would otherwise also PATCH the title: nothing may be sent.
	_, err := p.EditIssue(ctx, "acme", "", 2, provider.EditIssueOptions{Title: "x", RemoveLabels: []string{"bug"}})
	wantCode(t, err, exitcode.NotImplemented, "always has a type")
	none := int64(0)
	_, err = p.EditIssue(ctx, "acme", "", 2, provider.EditIssueOptions{Title: "x", Milestone: &none})
	wantCode(t, err, exitcode.NotImplemented, "milestones")
	_, err = p.EditIssue(ctx, "acme", "", 2, provider.EditIssueOptions{Title: "x", AddLabels: []string{"a", "b"}})
	wantCode(t, err, exitcode.Usage, "")
	_, err = p.EditIssue(ctx, "acme", "", 2, provider.EditIssueOptions{Title: "x", Assignees: []string{"a", "b"}})
	wantCode(t, err, exitcode.Usage, "")
}

func TestSetIssuePriority(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		doNow   bool
	}{{"issue_priority_on", true}, {"issue_priority_off", false}} {
		got, err := replay(t, tc.fixture).SetIssuePriority(context.Background(), "acme", "", 2, tc.doNow)
		if err != nil {
			t.Fatal(err)
		}
		if got.DoNow != tc.doNow {
			t.Errorf("%s: DoNow = %v", tc.fixture, got.DoNow)
		}
	}
}

func numbers(issues []types.Issue) []int {
	out := make([]int, len(issues))
	for i, it := range issues {
		out[i] = it.Number
	}
	return out
}
