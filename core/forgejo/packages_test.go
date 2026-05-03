package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func packageJSON(typ, name, version, owner string) map[string]any {
	return map[string]any{
		"id":         1,
		"type":       typ,
		"name":       name,
		"version":    version,
		"owner":      map[string]any{"login": owner},
		"created_at": "2026-04-01T00:00:00Z",
	}
}

func TestListPackages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages/o" {
			t.Errorf("path: %q, want /packages/o", r.URL.Path)
		}
		if got := r.URL.Query().Get("type"); got != "generic" {
			t.Errorf("type query: %q", got)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			packageJSON("generic", "alpha", "1.0.0", "o"),
			packageJSON("generic", "beta", "0.2.0", "o"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, page, err := p.ListPackages(context.Background(), "o", provider.ListPackagesOptions{
		Type: "generic",
	})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Type != "generic" || got[0].Name != "alpha" || got[0].Version != "1.0.0" || got[0].Owner != "o" {
		t.Errorf("first package: %+v", got[0])
	}
	if page == nil {
		t.Errorf("page should be non-nil")
	}
}

func TestListPackagesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListPackages(context.Background(), "missing", provider.ListPackagesOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound", got)
	}
}

func TestListPackagesAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListPackages(context.Background(), "o", provider.ListPackagesOptions{})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth", got)
	}
}

func TestGetPackage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/packages/o/generic/alpha/1.0.0"
		if r.URL.Path != want {
			t.Errorf("path: %q, want %q", r.URL.Path, want)
		}
		_ = json.NewEncoder(w).Encode(packageJSON("generic", "alpha", "1.0.0", "o"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPackage(context.Background(), "o", "generic", "alpha", "1.0.0")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Name != "alpha" || got.Version != "1.0.0" || got.Type != "generic" || got.Owner != "o" {
		t.Errorf("got %+v", got)
	}
}

func TestGetPackageNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetPackage(context.Background(), "o", "generic", "missing", "1.0.0")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound", got)
	}
}

func TestGetPackageAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetPackage(context.Background(), "o", "generic", "alpha", "1.0.0")
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth", got)
	}
}

func TestDeletePackage(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %q", r.Method)
		}
		deletePath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeletePackage(context.Background(), "o", "generic", "alpha", "1.0.0"); err != nil {
		t.Fatalf("DeletePackage: %v", err)
	}
	if deletePath != "/packages/o/generic/alpha/1.0.0" {
		t.Errorf("DELETE path: got %q", deletePath)
	}
}

func TestDeletePackageNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeletePackage(context.Background(), "o", "generic", "missing", "1.0.0")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound", got)
	}
}

func TestDeletePackageAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeletePackage(context.Background(), "o", "generic", "alpha", "1.0.0")
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth", got)
	}
}

// TestListPackagesWithFilters covers q + type + limit query encoding,
// confirming each option lands in the URL.
func TestListPackagesWithFilters(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, _, err := p.ListPackages(context.Background(), "o", provider.ListPackagesOptions{
		Type:  "npm",
		Q:     "foo",
		Limit: 5,
	}); err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	for _, want := range []string{"type=npm", "q=foo", "limit=5"} {
		if !strings.Contains(capturedQuery, want) {
			t.Errorf("query %q missing %q", capturedQuery, want)
		}
	}
}

// TestGetPackageEscapesPathSegments pins URL escaping for names that
// contain "/" (notably for npm scoped packages — "@scope/pkg" must
// have its slash escaped, otherwise it'd split into two path
// segments and the request would 404). Other characters that are
// path-legal (like "@", "+") pass through; what matters is the
// segment boundary doesn't move.
func TestGetPackageEscapesPathSegments(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(packageJSON("npm", "@scope/pkg", "1.0.0+build", "o"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.GetPackage(context.Background(), "o", "npm", "@scope/pkg", "1.0.0+build"); err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	// `/` in the name must be escaped as %2F so the segment count
	// stays correct (otherwise the URL would have an extra path
	// segment and Forgejo would 404).
	if !strings.Contains(capturedPath, "%2F") {
		t.Errorf("path should escape '/' inside name segment; got %q", capturedPath)
	}
}
