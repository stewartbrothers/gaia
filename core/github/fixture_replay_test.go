package github_test

import (
	"context"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

// Fixture-driven tests run the github provider against recorded
// api.github.com responses (testdata/fixtures/) so the provider's
// trim/decode pipeline is exercised on real wire-shapes — catches
// drift between hand-rolled httptest fixtures and what GitHub
// actually returns. Re-record with scripts/record-gh-fixtures.sh.

func TestListIssuesFromFixture(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"": "cli-cli-issues-list.json",
	})

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssues(context.Background(), "cli", "cli", provider.ListIssuesOptions{
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	// The fixture is real /repos/cli/cli/issues?state=open&per_page=5
	// captured via scripts/record-gh-fixtures.sh. We can't pin the
	// exact issue numbers (they change as cli/cli moves), but the
	// trim contract — every issue has Number/Title/State/Author —
	// must hold for every record.
	if len(got) == 0 {
		t.Fatal("expected at least one issue in fixture")
	}
	for i, iss := range got {
		if iss.Number == 0 {
			t.Errorf("[%d] missing Number: %+v", i, iss)
		}
		if iss.Title == "" {
			t.Errorf("[%d] missing Title: %+v", i, iss)
		}
		if iss.State == "" {
			t.Errorf("[%d] missing State: %+v", i, iss)
		}
		if iss.Author.Login == "" {
			t.Errorf("[%d] missing Author.Login: %+v", i, iss)
		}
	}
}
