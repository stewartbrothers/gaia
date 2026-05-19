package forgejo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func milestoneJSON(id int64, title, state string) map[string]any {
	return map[string]any{
		"id":            id,
		"title":         title,
		"description":   "sprint goals",
		"state":         state,
		"open_issues":   3,
		"closed_issues": 2,
		"created_at":    "2026-04-01T00:00:00Z",
		"updated_at":    "2026-04-02T00:00:00Z",
		"due_on":        "2026-05-01T00:00:00Z",
	}
}

func TestListMilestonesHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/milestones" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("state query: got %q want open", r.URL.Query().Get("state"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			milestoneJSON(1, "v0.4.0", "open"),
			milestoneJSON(2, "v0.5.0", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListMilestones(context.Background(), "o", "r", provider.ListMilestonesOptions{State: "open"})
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Title != "v0.4.0" || got[0].State != "open" {
		t.Errorf("[0] got %+v", got[0])
	}
	if got[0].OpenIssues != 3 || got[0].ClosedIssues != 2 {
		t.Errorf("counts: got open=%d closed=%d", got[0].OpenIssues, got[0].ClosedIssues)
	}
	if got[0].Description != "" {
		t.Errorf("list shape must trim Description; got %q", got[0].Description)
	}
}

func TestListMilestonesPassesNameFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "v0.4" {
			t.Errorf("name query: got %q", r.URL.Query().Get("name"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			milestoneJSON(1, "v0.4.0", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListMilestones(context.Background(), "o", "r", provider.ListMilestonesOptions{Name: "v0.4"})
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
}

func TestListMilestonesAuthError(t *testing.T) {
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

func TestGetMilestoneHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/milestones/7" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(milestoneJSON(7, "v0.4.0", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetMilestone(context.Background(), "o", "r", 7)
	if err != nil {
		t.Fatalf("GetMilestone: %v", err)
	}
	if got.ID != 7 || got.Title != "v0.4.0" {
		t.Errorf("got %+v", got)
	}
	if got.DueOn == nil {
		t.Errorf("due_on must be set; got nil")
	}
}

func TestGetMilestoneNotFound(t *testing.T) {
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

func TestCreateMilestone(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		if r.URL.Path != "/repos/o/r/milestones" {
			t.Errorf("path: %q", r.URL.Path)
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(milestoneJSON(42, "Sprint 3", "open"))
	}))
	defer srv.Close()

	due := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	p := newTestProvider(t, srv.URL)
	out, err := p.CreateMilestone(context.Background(), "o", "r", provider.CreateMilestoneOptions{
		Title:       "Sprint 3",
		Description: "May sprint",
		DueOn:       &due,
	})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if out.ID != 42 {
		t.Errorf("got %+v", out)
	}
	if !strings.Contains(string(capturedBody), `"title":"Sprint 3"`) {
		t.Errorf("body should contain title; got %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), "due_on") {
		t.Errorf("body should contain due_on; got %s", capturedBody)
	}
}

func TestCreateMilestoneAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.CreateMilestone(context.Background(), "o", "r", provider.CreateMilestoneOptions{
		Title: "x",
	})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got %d want Auth", got)
	}
}

func TestEditMilestonePatch(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: %q", r.Method)
		}
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(milestoneJSON(7, "Updated", "closed"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.EditMilestone(context.Background(), "o", "r", 7, provider.EditMilestoneOptions{
		Title: "Updated",
		State: "closed",
	})
	if err != nil {
		t.Fatalf("EditMilestone: %v", err)
	}
	if capturedPath != "/repos/o/r/milestones/7" {
		t.Errorf("path: got %q", capturedPath)
	}
	if out.State != "closed" {
		t.Errorf("state: got %q", out.State)
	}
}

func TestEditMilestoneNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.EditMilestone(context.Background(), "o", "r", 999, provider.EditMilestoneOptions{
		State: "closed",
	})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d want NotFound", got)
	}
}

func TestDeleteMilestone(t *testing.T) {
	var deletePath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %q", r.Method)
		}
		deletePath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteMilestone(context.Background(), "o", "r", 7); err != nil {
		t.Fatalf("DeleteMilestone: %v", err)
	}
	if deletePath != "/repos/o/r/milestones/7" {
		t.Errorf("DELETE path: got %q", deletePath)
	}
}

func TestDeleteMilestoneNotFound(t *testing.T) {
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

func TestListMilestoneIssues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if r.URL.Query().Get("milestones") != "7" {
			t.Errorf("milestones query: got %q want 7", r.URL.Query().Get("milestones"))
		}
		if r.URL.Query().Get("type") != "issues" {
			t.Errorf("type query: got %q", r.URL.Query().Get("type"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 100, "title": "do the thing", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListMilestoneIssues(context.Background(), "o", "r", 7, provider.ListMilestoneIssuesOptions{})
	if err != nil {
		t.Fatalf("ListMilestoneIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 100 {
		t.Errorf("got %+v", got)
	}
}

func TestListMilestoneIssuesAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListMilestoneIssues(context.Background(), "o", "r", 7, provider.ListMilestoneIssuesOptions{})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got %d want Auth", got)
	}
}
