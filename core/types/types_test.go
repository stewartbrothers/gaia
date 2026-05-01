package types_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/types"
)

func TestSchemaVersion(t *testing.T) {
	if types.SchemaVersion == "" {
		t.Fatal("SchemaVersion must not be empty")
	}
}

func TestIssueRoundTrip(t *testing.T) {
	closed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	in := types.Issue{
		Number:    42,
		Title:     "test issue",
		State:     "closed",
		Author:    types.User{Login: "alice"},
		Labels:    []types.Label{{Name: "bug"}, {Name: "p1"}},
		Assignees: []types.User{{Login: "bob"}},
		Body:      "body text",
		CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
		ClosedAt:  &closed,
	}
	out := jsonRoundTrip(t, in, &types.Issue{}).(*types.Issue)

	if out.Number != in.Number {
		t.Errorf("number: got %d, want %d", out.Number, in.Number)
	}
	if out.Title != in.Title {
		t.Errorf("title: got %q, want %q", out.Title, in.Title)
	}
	if out.State != in.State {
		t.Errorf("state: got %q, want %q", out.State, in.State)
	}
	if out.Author.Login != in.Author.Login {
		t.Errorf("author: got %q, want %q", out.Author.Login, in.Author.Login)
	}
	if len(out.Labels) != 2 || out.Labels[0].Name != "bug" || out.Labels[1].Name != "p1" {
		t.Errorf("labels: got %+v", out.Labels)
	}
	if len(out.Assignees) != 1 || out.Assignees[0].Login != "bob" {
		t.Errorf("assignees: got %+v", out.Assignees)
	}
	if out.ClosedAt == nil || !out.ClosedAt.Equal(closed) {
		t.Errorf("closed_at: got %v, want %v", out.ClosedAt, closed)
	}
}

func TestPullRequestRoundTrip(t *testing.T) {
	merged := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	mergeable := true
	in := types.PullRequest{
		Number:    7,
		Title:     "feat: things",
		State:     "merged",
		Author:    types.User{Login: "alice"},
		Head:      types.BranchRef{Ref: "feature/x", SHA: "deadbeef"},
		Base:      types.BranchRef{Ref: "main", SHA: "cafebabe"},
		Draft:     false,
		Mergeable: &mergeable,
		MergedAt:  &merged,
		CISummary: &types.CISummary{
			State: "success", Total: 3, Successful: 3,
		},
	}
	out := jsonRoundTrip(t, in, &types.PullRequest{}).(*types.PullRequest)

	if out.Number != in.Number || out.Title != in.Title || out.State != in.State {
		t.Errorf("scalars: got %+v", out)
	}
	if out.Head.Ref != "feature/x" || out.Head.SHA != "deadbeef" {
		t.Errorf("head: got %+v", out.Head)
	}
	if out.Mergeable == nil || *out.Mergeable != true {
		t.Errorf("mergeable: got %v", out.Mergeable)
	}
	if out.CISummary == nil || out.CISummary.State != "success" || out.CISummary.Total != 3 {
		t.Errorf("ci_summary: got %+v", out.CISummary)
	}
}

func TestCommentRoundTrip(t *testing.T) {
	in := types.Comment{
		ID:        101,
		Source:    "inline",
		Author:    types.User{Login: "alice"},
		Body:      "nit: rename this",
		Path:      "core/types/issue.go",
		Line:      42,
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
	}
	out := jsonRoundTrip(t, in, &types.Comment{}).(*types.Comment)

	if out.ID != in.ID || out.Source != in.Source || out.Body != in.Body {
		t.Errorf("scalars: got %+v", out)
	}
	if out.Path != in.Path || out.Line != in.Line {
		t.Errorf("inline location: got path=%q line=%d", out.Path, out.Line)
	}
}

func TestDiffFileAndHunkRoundTrip(t *testing.T) {
	in := types.DiffFile{
		Path:    "core/types/issue.go",
		OldPath: "core/types/old_issue.go",
		Status:  "renamed",
		Binary:  false,
		Hunks: []types.Hunk{
			{
				Header:   "@@ -1,3 +1,4 @@",
				OldStart: 1, OldLines: 3,
				NewStart: 1, NewLines: 4,
				Lines: []string{" package types", "+", "-old", "+new"},
			},
		},
	}
	out := jsonRoundTrip(t, in, &types.DiffFile{}).(*types.DiffFile)

	if out.Path != in.Path || out.OldPath != in.OldPath || out.Status != in.Status {
		t.Errorf("scalars: got %+v", out)
	}
	if len(out.Hunks) != 1 {
		t.Fatalf("hunks: got %d, want 1", len(out.Hunks))
	}
	h := out.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 4 {
		t.Errorf("hunk scalars: got %+v", h)
	}
	if len(h.Lines) != 4 || !strings.HasPrefix(h.Lines[0], " ") {
		t.Errorf("hunk lines: got %+v", h.Lines)
	}
}

func TestDiffFileBinary(t *testing.T) {
	in := types.DiffFile{Path: "logo.png", Status: "modified", Binary: true}
	out := jsonRoundTrip(t, in, &types.DiffFile{}).(*types.DiffFile)
	if !out.Binary || len(out.Hunks) != 0 {
		t.Errorf("binary diff should marshal with binary=true and no hunks; got %+v", out)
	}
}

func TestSearchResultRoundTrip(t *testing.T) {
	in := types.SearchResult{
		Kind:     "pull_request",
		Number:   57,
		Title:    "lint: add golangci-lint config",
		RepoFull: "Gerwood/gaia",
	}
	out := jsonRoundTrip(t, in, &types.SearchResult{}).(*types.SearchResult)

	if out.Kind != in.Kind || out.Number != in.Number || out.Title != in.Title || out.RepoFull != in.RepoFull {
		t.Errorf("got %+v, want %+v", out, in)
	}
}

// TestNoSurpriseFieldNames asserts the JSON wire format uses snake_case keys
// for the multi-word fields that agents will branch on. This guards against
// silent rename in a future refactor.
func TestNoSurpriseFieldNames(t *testing.T) {
	pr := types.PullRequest{Number: 1, CreatedAt: time.Now()}
	b, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)
	for _, want := range []string{`"number"`, `"created_at"`} {
		if !strings.Contains(wire, want) {
			t.Errorf("expected key %s in %s", want, wire)
		}
	}
	// Reject CamelCase Go field names leaking onto the wire.
	for _, banned := range []string{`"Number"`, `"CreatedAt"`, `"CISummary"`} {
		if strings.Contains(wire, banned) {
			t.Errorf("unexpected camelCase key %s in %s", banned, wire)
		}
	}
}

// jsonRoundTrip marshals in, unmarshals into out, returns out for type
// assertion. Centralised so the per-type tests stay focused on field
// assertions.
func jsonRoundTrip(t *testing.T, in any, out any) any {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal: %v\nwire: %s", err, b)
	}
	return out
}
