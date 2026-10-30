package aikestra

import (
	"context"
	"net/url"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// Issues belong to an Aikestra Project and only optionally reference a
// Repo, so every method here addresses /projects/{owner}/issues and
// ignores `repo`: `gaia issue list --repo acme/web` lists all of acme's
// Issues. Aikestra has no labels; a gaia label maps onto the Issue's
// single `type`, and an Issue has at most one assignee.

// apiIssue is Aikestra's Issue schema.
type apiIssue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Type      string     `json:"type"`
	State     string     `json:"state"`
	Priority  bool       `json:"priority"`
	Assignee  *string    `json:"assignee"`
	Author    *string    `json:"author"`
	Parent    *int       `json:"parent"`
	Board     string     `json:"board"`
	Stage     string     `json:"stage"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at"`
}

func (a apiIssue) toType() types.Issue {
	out := types.Issue{
		Number:    a.Number,
		Title:     a.Title,
		State:     a.State,
		Body:      a.Body,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
		ClosedAt:  a.ClosedAt,
		Type:      a.Type,
		DoNow:     a.Priority,
		Board:     a.Board,
		Stage:     a.Stage,
	}
	if a.Author != nil {
		out.Author = types.User{Login: *a.Author}
	}
	if a.Assignee != nil {
		out.Assignees = []types.User{{Login: *a.Assignee}}
	}
	if a.Parent != nil {
		out.Parent = *a.Parent
	}
	return out
}

// ListIssues lists the Project's Issues, newest first. One label filters
// by type. Aikestra's filters are state, type, assignee (email, "me" or
// "none") and a title substring; there is no author or since filter, and
// filtering client-side would break the server's cursor, so those are
// refused rather than emulated.
func (p *Provider) ListIssues(ctx context.Context, owner, _ string, opts provider.ListIssuesOptions) ([]types.Issue, *provider.Page, error) {
	if opts.Author != "" {
		return nil, nil, notImplemented("filtering issues by author")
	}
	if !opts.Since.IsZero() {
		return nil, nil, notImplemented("filtering issues by update time")
	}
	issueType, err := typeFromLabels(opts.Labels)
	if err != nil {
		return nil, nil, err
	}
	q := url.Values{}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if issueType != "" {
		q.Set("type", issueType)
	}
	if opts.Assignee != "" {
		q.Set("assignee", meAlias(opts.Assignee))
	}
	if opts.Query != "" {
		q.Set("q", opts.Query)
	}
	page, err := getPage[apiIssue](ctx, p.client, projectPath(owner, "/issues"), q, opts.Limit, opts.Cursor)
	if err != nil {
		return nil, nil, err
	}
	out := make([]types.Issue, len(page.Items))
	for i, it := range page.Items {
		out[i] = it.toType()
	}
	return out, pageOf(page.NextCursor), nil
}

// GetIssue fetches Issue n. WithComments inlines the most recent N
// comments; WithBlockers/WithBlocks inline the `blocked_by`/`blocks`
// relationships (one relationships walk serves both).
func (p *Provider) GetIssue(ctx context.Context, owner, repo string, n int, opts provider.GetIssueOptions) (*types.Issue, error) {
	var raw apiIssue
	if err := p.client.Get(ctx, issuePath(owner, n, ""), nil, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	if opts.WithComments > 0 {
		comments, err := p.ListComments(ctx, owner, repo, n, provider.ListCommentsOptions{})
		if err != nil {
			return nil, err
		}
		if len(comments) > opts.WithComments {
			comments = comments[len(comments)-opts.WithComments:]
		}
		out.Comments = comments
	}
	if opts.WithBlockers > 0 || opts.WithBlocks > 0 {
		rels, err := p.relationships(ctx, owner, n)
		if err != nil {
			return nil, err
		}
		if opts.WithBlockers > 0 {
			out.Blockers = capIssues(relatedIssues(rels, relBlockedBy), opts.WithBlockers)
		}
		if opts.WithBlocks > 0 {
			out.Blocks = capIssues(relatedIssues(rels, relBlocks), opts.WithBlocks)
		}
	}
	return &out, nil
}

// apiCreateIssue is POST /projects/{slug}/issues. The server rejects
// unknown keys, so only documented ones are sent.
type apiCreateIssue struct {
	Title    string  `json:"title"`
	Body     string  `json:"body,omitempty"`
	Type     string  `json:"type,omitempty"`
	Assignee *string `json:"assignee,omitempty"`
}

// CreateIssue opens an Issue on the Project. One label becomes its type
// (the Project's default, `task`, otherwise); at most one assignee
// (an email or "me").
func (p *Provider) CreateIssue(ctx context.Context, owner, _ string, opts provider.CreateIssueOptions) (*types.Issue, error) {
	if opts.Milestone != 0 {
		return nil, notImplemented("milestones")
	}
	issueType, err := typeFromLabels(opts.Labels)
	if err != nil {
		return nil, err
	}
	assignee, err := singleAssignee(opts.Assignees)
	if err != nil {
		return nil, err
	}
	body := apiCreateIssue{Title: opts.Title, Body: opts.Body, Type: issueType}
	if assignee != "" {
		body.Assignee = &assignee
	}
	var raw apiIssue
	if err := p.client.Post(ctx, projectPath(owner, "/issues"), body, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// apiPatchIssue is PATCH /projects/{slug}/issues/{n}: title, body, type.
type apiPatchIssue struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	Type  string `json:"type,omitempty"`
}

// apiSetState is PUT …/state; apiSetAssignee is PUT …/assignee. Note is
// the "why", required when the Project requires notes (`note_required`).
type apiSetState struct {
	State string  `json:"state"`
	Note  *string `json:"note,omitempty"`
}

type apiSetAssignee struct {
	Assignee string  `json:"assignee"`
	Note     *string `json:"note,omitempty"`
}

// EditIssue applies opts across Aikestra's per-concern endpoints: PATCH
// for title/body/type, then PUT state, then PUT assignee. Everything is
// validated before the first write, so an unsupported option never
// leaves a half-applied edit. With nothing to change it returns the
// current Issue.
//
// AddLabels takes one label, which becomes the type. RemoveLabels is
// refused: an Aikestra Issue always has a type, so set a different one
// instead. opts.Note travels with the state and assignee changes.
func (p *Provider) EditIssue(ctx context.Context, owner, repo string, n int, opts provider.EditIssueOptions) (*types.Issue, error) {
	if opts.Milestone != nil {
		return nil, notImplemented("milestones")
	}
	if len(opts.RemoveLabels) > 0 {
		return nil, exitcode.Errorf(exitcode.NotImplemented,
			"an Aikestra Issue always has a type; set a different one with --add-label <type> instead of removing it")
	}
	issueType, err := typeFromLabels(opts.AddLabels)
	if err != nil {
		return nil, err
	}
	assignee, err := singleAssignee(opts.Assignees)
	if err != nil {
		return nil, err
	}
	var note *string
	if opts.Note != "" {
		note = &opts.Note
	}

	var raw *apiIssue
	if opts.Title != "" || opts.Body != "" || issueType != "" {
		raw = &apiIssue{}
		body := apiPatchIssue{Title: opts.Title, Body: opts.Body, Type: issueType}
		if err := p.client.Patch(ctx, issuePath(owner, n, ""), body, raw); err != nil {
			return nil, err
		}
	}
	if opts.State != "" {
		raw = &apiIssue{}
		if err := p.client.Put(ctx, issuePath(owner, n, "/state"), apiSetState{State: opts.State, Note: note}, raw); err != nil {
			return nil, err
		}
	}
	if assignee != "" {
		raw = &apiIssue{}
		if err := p.client.Put(ctx, issuePath(owner, n, "/assignee"), apiSetAssignee{Assignee: assignee, Note: note}, raw); err != nil {
			return nil, err
		}
	}
	if raw == nil {
		return p.GetIssue(ctx, owner, repo, n, provider.GetIssueOptions{})
	}
	out := raw.toType()
	return &out, nil
}

// SetIssuePriority turns Issue n's Do Now flag on or off.
func (p *Provider) SetIssuePriority(ctx context.Context, owner, _ string, n int, doNow bool) (*types.Issue, error) {
	body := struct {
		Priority bool `json:"priority"`
	}{Priority: doNow}
	var raw apiIssue
	if err := p.client.Put(ctx, issuePath(owner, n, "/priority"), body, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// typeFromLabels maps gaia labels onto an Aikestra Issue type. An Issue
// has exactly one type, so more than one label is a usage error. Whether
// the name is one of the Project's types is the server's call (422
// validation_failed otherwise) — types are configurable per Project.
func typeFromLabels(labels []string) (string, error) {
	switch len(labels) {
	case 0:
		return "", nil
	case 1:
		return labels[0], nil
	}
	return "", exitcode.Errorf(exitcode.Usage,
		"Aikestra has no labels: an Issue has one type, so pass a single --label (got %d)", len(labels))
}

// singleAssignee enforces Aikestra's one-assignee model.
func singleAssignee(assignees []string) (string, error) {
	switch len(assignees) {
	case 0:
		return "", nil
	case 1:
		return meAlias(assignees[0]), nil
	}
	return "", exitcode.Errorf(exitcode.Usage,
		"an Aikestra Issue has one assignee (got %d)", len(assignees))
}

// pageOf maps Aikestra's next_cursor onto gaia's Page. The cursor is
// authoritative: null means nothing follows, even after a full page.
func pageOf(next *string) *provider.Page {
	if next == nil {
		return &provider.Page{}
	}
	return &provider.Page{Truncated: true, NextCursor: *next}
}

// meAlias maps gaia's gh/tea-style "@me" onto Aikestra's "me".
func meAlias(who string) string {
	if who == "@me" {
		return "me"
	}
	return who
}
