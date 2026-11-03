package forgejo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// Issue type is an Aikestra concept (#404). Here it must be refused
// before any request, never dropped silently: an ignored --type filter
// would return the wrong Issues.
func TestIssueType_RefusedBeforeAnyRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	p := newTestProvider(t, srv.URL)
	ctx := context.Background()

	_, _, err := p.ListIssues(ctx, "o", "r", provider.ListIssuesOptions{Type: "bug"})
	if exitcode.Of(err) != exitcode.NotImplemented {
		t.Errorf("ListIssues: err %v, want NotImplemented", err)
	}
	_, err = p.CreateIssue(ctx, "o", "r", provider.CreateIssueOptions{Title: "x", Type: "bug"})
	if exitcode.Of(err) != exitcode.NotImplemented {
		t.Errorf("CreateIssue: err %v, want NotImplemented", err)
	}
	_, err = p.EditIssue(ctx, "o", "r", 1, provider.EditIssueOptions{Type: "bug"})
	if exitcode.Of(err) != exitcode.NotImplemented {
		t.Errorf("EditIssue: err %v, want NotImplemented", err)
	}
}
