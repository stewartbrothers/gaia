package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

func TestServerVersionHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			t.Errorf("path: %q, want /version", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "15.0.1"})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ServerVersion(context.Background())
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	if got.Version != "15.0.1" {
		t.Errorf("version: got %q, want %q", got.Version, "15.0.1")
	}
}

func TestServerVersionNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.ServerVersion(context.Background())
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound", got)
	}
}
