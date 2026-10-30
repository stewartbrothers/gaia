package aikestra

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func TestListComments(t *testing.T) {
	got, err := replay(t, "comments").ListComments(context.Background(), "acme", "web", 1, provider.ListCommentsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Body != "First look" || got[1].Author.Login != "bob@example.com" || got[0].Source != "issue" {
		t.Fatalf("got %+v", got)
	}
}

func TestListComments_Limit(t *testing.T) {
	got, err := replay(t, "comments").ListComments(context.Background(), "acme", "", 1, provider.ListCommentsOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "First look" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetPage_FollowsCursorToNull(t *testing.T) {
	p := replay(t, "comments_page1", "comments_page2")
	ctx := context.Background()
	path := issuePath("acme", 1, "/comments")
	first, err := getPage[apiComment](ctx, p.client, path, nil, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("page 1: %+v", first)
	}
	second, err := getPage[apiComment](ctx, p.client, path, nil, 1, *first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	// Exactly full and last: only the null cursor says so.
	if len(second.Items) != 1 || second.NextCursor != nil {
		t.Fatalf("page 2: %+v", second)
	}
}

func TestWalk_StopsAtMaxPages(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"items":[{"body":"x"}],"next_cursor":"more"}`))
	}))
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL})
	items, truncated, err := walk[apiComment](context.Background(), c, "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(items) != maxWalkPages || int(calls.Load()) != maxWalkPages {
		t.Fatalf("truncated=%v items=%d calls=%d", truncated, len(items), calls.Load())
	}
}

func TestCreateIssueComment(t *testing.T) {
	got, err := replay(t, "comment_create").CreateIssueComment(context.Background(), "acme", "", 1, "First look")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "First look" || got.Author.Login != "ada@example.com" {
		t.Fatalf("got %+v", got)
	}
}

func TestDependencies_List(t *testing.T) {
	ctx := context.Background()
	blockers, _, err := replay(t, "relationships").ListIssueDependencies(ctx, "acme", "", 3, provider.ListIssueDepsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 || blockers[0].Number != 1 || blockers[0].Title != "Login is slow" {
		t.Errorf("blockers %+v", blockers)
	}
	blocks, _, err := replay(t, "relationships").ListIssueBlocks(ctx, "acme", "", 3, provider.ListIssueDepsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// child, related_to and the like are not dependencies.
	if len(blocks) != 1 || blocks[0].Number != 4 {
		t.Errorf("blocks %+v", blocks)
	}
}

func TestDependencies_InlinedOnGetIssue(t *testing.T) {
	got, err := replay(t, "issue_get_blocked", "relationships").GetIssue(context.Background(), "acme", "", 3, provider.GetIssueOptions{WithBlockers: 5, WithBlocks: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blockers) != 1 || len(got.Blocks) != 1 {
		t.Errorf("blockers %+v blocks %+v", got.Blockers, got.Blocks)
	}
}

func TestDependencies_Add(t *testing.T) {
	got, err := replay(t, "relationship_add_blocked_by").AddIssueDependency(context.Background(), "acme", "web", 3, provider.IssueDepRef{Number: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestDependencies_AddCycle(t *testing.T) {
	_, err := replay(t, "cycle").AddIssueDependency(context.Background(), "acme", "", 1, provider.IssueDepRef{Number: 3})
	wantCode(t, err, exitcode.Generic, "cycle")
}

func TestDependencies_SameProjectOnly(t *testing.T) {
	p := replay(t)
	ref := provider.IssueDepRef{Owner: "other", Repo: "web", Number: 1}
	_, err := p.AddIssueDependency(context.Background(), "acme", "", 3, ref)
	wantCode(t, err, exitcode.NotImplemented, "one Project")
	wantCode(t, p.RemoveIssueDependency(context.Background(), "acme", "", 3, ref), exitcode.NotImplemented, "")
}

func TestDependencies_Remove(t *testing.T) {
	id := relationshipID(t, "blocked_by", 1)
	if path := fixture(t, "relationship_delete").Request.Path; path != "/projects/acme/issues/3/relationships/"+id {
		t.Fatalf("fixture deletes %s, not relationship %s", path, id)
	}
	err := replay(t, "relationships", "relationship_delete").RemoveIssueDependency(context.Background(), "acme", "", 3, provider.IssueDepRef{Number: 1})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDependencies_RemoveMissingEdge(t *testing.T) {
	err := replay(t, "relationships").RemoveIssueDependency(context.Background(), "acme", "", 3, provider.IssueDepRef{Number: 2})
	wantCode(t, err, exitcode.NotFound, "#2 does not block #3")
}

func TestBoards(t *testing.T) {
	ctx := context.Background()
	boards, page, err := replay(t, "boards").ListBoards(ctx, "acme", provider.ListBoardsOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 1 || boards[0].Name != "Main" || !boards[0].Default || len(boards[0].Stages) != 6 || page.Truncated {
		t.Fatalf("boards %+v page %+v", boards, page)
	}

	board, err := replay(t, "board").GetBoard(ctx, "acme", "main")
	if err != nil {
		t.Fatal(err)
	}
	if board.Stages[0].Name != "Todo" || !reflect.DeepEqual(board.Stages[0].Issues, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("board %+v", board)
	}

	_, err = replay(t, "board_missing").GetBoard(ctx, "acme", "nope")
	wantCode(t, err, exitcode.NotFound, "")
}

func TestMoveIssue(t *testing.T) {
	got, err := replay(t, "placement").MoveIssue(context.Background(), "acme", "", 2, provider.MoveIssueOptions{Stage: "dev", Board: "main", Note: "Starting"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Stage != "Dev" || got.Board != "Main" {
		t.Fatalf("got %+v", got)
	}
}

func TestMoveIssue_Blocked(t *testing.T) {
	_, err := replay(t, "placement_blocked").MoveIssue(context.Background(), "acme", "", 3, provider.MoveIssueOptions{Stage: "dev"})
	wantCode(t, err, exitcode.PolicyViolation, "Blocked by #1")
}

func TestMoveIssue_BadStage(t *testing.T) {
	_, err := replay(t, "placement_bad_stage").MoveIssue(context.Background(), "acme", "", 2, provider.MoveIssueOptions{Stage: "Nowhere"})
	wantCode(t, err, exitcode.Usage, "validation_failed")
}

func TestRefs(t *testing.T) {
	ctx := context.Background()
	branches, _, err := replay(t, "refs").ListBranches(ctx, "acme", "app", provider.ListBranchesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[1].Name != "main" || branches[1].Commit == "" {
		t.Fatalf("branches %+v", branches)
	}
	tags, _, err := replay(t, "refs").ListTags(ctx, "acme", "app", provider.ListTagsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "v1" {
		t.Fatalf("tags %+v", tags)
	}
	capped, page, err := replay(t, "refs").ListBranches(ctx, "acme", "app", provider.ListBranchesOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != 1 || !page.Truncated {
		t.Fatalf("capped %+v page %+v", capped, page)
	}
}

// Every documented error code maps onto a gaia exit code. Each case is a
// recorded response except internal_error, which the server can't be
// made to produce on demand.
func TestErrorMapping(t *testing.T) {
	cases := []struct {
		fixture string
		code    int
	}{
		{"invalid_request", exitcode.Usage},
		{"invalid_fields", exitcode.Usage},
		{"invalid_cursor", exitcode.Usage},
		{"unauthenticated", exitcode.Auth},
		{"forbidden", exitcode.Auth},
		{"issue_get_missing", exitcode.NotFound},
		{"issue_get_no_access", exitcode.NotFound},
		{"method_not_allowed", exitcode.Usage},
		{"conflict", exitcode.Generic},
		{"placement_blocked", exitcode.PolicyViolation},
		{"cycle", exitcode.Generic},
		{"unsupported_media_type", exitcode.Usage},
		{"validation_failed", exitcode.Usage},
		{"relationship_add_missing", exitcode.Usage},
		{"note_required", exitcode.Usage},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			ex := fixture(t, tc.fixture)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(ex.Response.Status)
				_, _ = w.Write(ex.Response.Body)
			}))
			defer srv.Close()
			err := New(Options{BaseURL: srv.URL}).Get(context.Background(), "/x", nil, nil)
			wantCode(t, err, tc.code, "")
		})
	}
	if code, _ := mapError(500, "internal_error"); code != exitcode.Network {
		t.Errorf("internal_error → %d", code)
	}
	if code, _ := mapError(418, "brand_new_code"); code != exitcode.Generic {
		t.Errorf("unknown code → %d", code)
	}
}

func TestErrors_NonJSONBodyFallsBackToStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>gone</html>"))
	}))
	defer srv.Close()
	err := New(Options{BaseURL: srv.URL}).Get(context.Background(), "/x", nil, nil)
	wantCode(t, err, exitcode.NotFound, "gone")
}

func TestGet_RetriesOnce5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"boom"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"email":"ada@example.com"}`))
	}))
	defer srv.Close()
	p := NewProvider(Options{BaseURL: srv.URL, RetryWait: time.Millisecond})
	got, err := p.Whoami(context.Background())
	if err != nil || got != "ada@example.com" || calls.Load() != 2 {
		t.Fatalf("got %q err %v calls %d", got, err, calls.Load())
	}
}

func TestWrites_NeverRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"boom"}}`))
	}))
	defer srv.Close()
	_, err := NewProvider(Options{BaseURL: srv.URL}).CreateIssueComment(context.Background(), "acme", "", 1, "x")
	wantCode(t, err, exitcode.Network, "internal_error")
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestRegistration(t *testing.T) {
	if _, err := provider.Build("aikestra", provider.BuildConfig{}); exitcode.Of(err) != exitcode.Usage {
		t.Fatalf("missing API URL: %v", err)
	}
	if _, err := provider.Build("aikestra", provider.BuildConfig{APIURL: "http://x/api/v1"}); err != nil {
		t.Fatal(err)
	}
	if got := provider.TokenEnvNames("aikestra"); !reflect.DeepEqual(got, []string{"AIKESTRA_TOKEN"}) {
		t.Errorf("token envs %v", got)
	}
	for _, c := range []provider.Capability{provider.CapPullRequests, provider.CapLabels, provider.CapSearch, provider.CapMilestones} {
		if provider.Supports("aikestra", c) {
			t.Errorf("aikestra claims %s", c)
		}
	}
	for _, c := range []provider.Capability{provider.CapBoards, provider.CapIssuePriority} {
		if !provider.Supports("aikestra", c) {
			t.Errorf("aikestra lacks %s", c)
		}
	}
}

func TestUnsupported_NotImplemented(t *testing.T) {
	p := replay(t)
	ctx := context.Background()
	_, _, err := p.ListPullRequests(ctx, "acme", "web", provider.ListPullRequestsOptions{})
	wantCode(t, err, exitcode.NotImplemented, "pull requests")
	_, err = p.ListLabels(ctx, "acme", "web", provider.ListLabelsOptions{})
	wantCode(t, err, exitcode.NotImplemented, "labels")
	_, _, err = p.Search(ctx, "q", provider.SearchOptions{})
	wantCode(t, err, exitcode.NotImplemented, "search")
	_, err = p.CreateBranch(ctx, "acme", "web", "b", provider.CreateBranchOptions{})
	wantCode(t, err, exitcode.NotImplemented, "git")
	_, err = p.EditIssueComment(ctx, "acme", "web", 1, "x")
	wantCode(t, err, exitcode.NotImplemented, "")
}
