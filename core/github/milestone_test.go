package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func ghMilestoneJSON(id int64, number int, title, state string) map[string]any {
	return map[string]any{
		"id":            id,
		"number":        number,
		"title":         title,
		"description":   "sprint",
		"state":         state,
		"open_issues":   3,
		"closed_issues": 2,
		"created_at":    "2026-04-01T00:00:00Z",
		"updated_at":    "2026-04-02T00:00:00Z",
		"due_on":        "2026-05-01T00:00:00Z",
	}
}

func TestListMilestonesGHHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/milestones" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("state: %q", r.URL.Query().Get("state"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			ghMilestoneJSON(900, 1, "v0.4.0", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListMilestones(context.Background(), "o", "r", provider.ListMilestonesOptions{})
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		// ID is the Number from GitHub, not the internal db ID.
		t.Errorf("got %+v", got)
	}
}

func TestListMilestonesGHAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListMilestones(context.Background(), "o", "r", provider.ListMilestonesOptions{})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got %d want Auth", got)
	}
}

func TestGetMilestoneGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/milestones/3" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ghMilestoneJSON(900, 3, "v0.4.0", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetMilestone(context.Background(), "o", "r", 3)
	if err != nil {
		t.Fatalf("GetMilestone: %v", err)
	}
	// GitHub provider surfaces Number as ID.
	if got.ID != 3 {
		t.Errorf("ID: got %d want 3", got.ID)
	}
}

func TestGetMilestoneGHNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetMilestone(context.Background(), "o", "r", 999)
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d want NotFound", got)
	}
}

func TestCreateMilestoneGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(ghMilestoneJSON(1, 1, "Sprint", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.CreateMilestone(context.Background(), "o", "r", provider.CreateMilestoneOptions{Title: "Sprint"})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if out.ID != 1 {
		t.Errorf("got %+v", out)
	}
}

func TestEditMilestoneGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: %q", r.Method)
		}
		if r.URL.Path != "/repos/o/r/milestones/3" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ghMilestoneJSON(900, 3, "Closed", "closed"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.EditMilestone(context.Background(), "o", "r", 3, provider.EditMilestoneOptions{State: "closed"})
	if err != nil {
		t.Fatalf("EditMilestone: %v", err)
	}
	if out.State != "closed" {
		t.Errorf("got %+v", out)
	}
}

func TestDeleteMilestoneGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %q", r.Method)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteMilestone(context.Background(), "o", "r", 3); err != nil {
		t.Fatalf("DeleteMilestone: %v", err)
	}
}

func TestDeleteMilestoneGHNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeleteMilestone(context.Background(), "o", "r", 999)
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d want NotFound", got)
	}
}

func TestListMilestoneIssuesGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("milestone") != "3" {
			t.Errorf("milestone query: %q", r.URL.Query().Get("milestone"))
		}
		// Mix one real issue + one PR (PR should be filtered out).
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 50, "title": "real issue", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			},
			{
				"number": 51, "title": "a PR", "state": "open",
				"user":         map[string]any{"login": "alice"},
				"pull_request": map[string]any{},
				"created_at":   "2026-04-01T00:00:00Z",
				"updated_at":   "2026-04-02T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListMilestoneIssues(context.Background(), "o", "r", 3, provider.ListMilestoneIssuesOptions{})
	if err != nil {
		t.Fatalf("ListMilestoneIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 50 {
		t.Errorf("expected exactly 1 real issue; got %+v", got)
	}
}
