package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

func TestListRunners(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/runners" {
			t.Errorf("path: %q", r.URL.Path)
		}
		// Forgejo returns a bare array; labels here are a plain string slice.
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "runner-a", "status": "online", "busy": false, "labels": []string{"self-hosted", "linux"}},
			{"id": 2, "name": "runner-b", "status": "offline", "busy": true, "labels": []string{"docker"}},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListRunners(context.Background(), "o", "r", provider.ListRunnersOptions{})
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}
	if len(got) != 2 || got[0].Name != "runner-a" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Status != "online" || got[0].Busy {
		t.Errorf("runner-a status/busy: %+v", got[0])
	}
	if len(got[0].Labels) != 2 || got[0].Labels[0] != "self-hosted" {
		t.Errorf("labels not flattened: %+v", got[0].Labels)
	}
}

// TestListRunnersObjectLabels: Forgejo has also emitted labels as an
// object array [{name}]; the decoder must flatten that shape too.
func TestListRunnersObjectLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "runner-c", "status": "online", "busy": false, "labels": []map[string]any{
				{"name": "self-hosted"}, {"name": "arm64"},
			}},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListRunners(context.Background(), "o", "r", provider.ListRunnersOptions{})
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}
	if len(got) != 1 || len(got[0].Labels) != 2 || got[0].Labels[1] != "arm64" {
		t.Fatalf("object labels not flattened: %+v", got)
	}
}

// TestListRunnersOrg: --org switches to the org-level endpoint.
func TestListRunnersOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/o/actions/runners" {
			t.Errorf("org path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "org-runner", "status": "online"}})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListRunners(context.Background(), "o", "r", provider.ListRunnersOptions{Org: true})
	if err != nil {
		t.Fatalf("ListRunners org: %v", err)
	}
	if len(got) != 1 || got[0].Name != "org-runner" {
		t.Errorf("got %+v", got)
	}
}
