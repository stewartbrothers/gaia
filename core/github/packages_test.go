package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/github"
	"github.com/stewartbrothers/gaia/core/provider"
)

// userTypeJSON is the minimal GET /users/{owner} response we read to
// dispatch users-vs-orgs. type=="User" routes through /users/{owner};
// type=="Organization" routes through /orgs/{owner}.
func userTypeJSON(t string) map[string]any {
	return map[string]any{"login": "o", "type": t}
}

// pkgVersionJSON mirrors the GitHub /users/{o}/packages/{type}/{name}/versions/{vid} shape.
func pkgVersionJSON(id int64, name string) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"created_at": "2026-04-01T00:00:00Z",
	}
}

// pkgListEntryJSON mirrors a row in /users/{o}/packages or /orgs/{o}/packages.
// Each entry is a package family (no version); GitHub returns the
// most-recent version under "version_count" + lacks per-version fields.
// We trim into one types.Package per package, with Version = "" for
// list (unless the API surfaces a version_count→latest mapping —
// GitHub does not on the package list, only on the versions endpoint).
func pkgListEntryJSON(typ, name string) map[string]any {
	return map[string]any{
		"id":           1,
		"name":         name,
		"package_type": typ,
		"owner":        map[string]any{"login": "o"},
		"created_at":   "2026-04-01T00:00:00Z",
	}
}

// TestListPackagesUser routes /users/{owner}/packages when owner is a User.
func TestListPackagesUser(t *testing.T) {
	var hitOrgs bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("User"))
		case r.URL.Path == "/users/o/packages":
			if got := r.URL.Query().Get("package_type"); got != "container" {
				t.Errorf("package_type query: %q", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				pkgListEntryJSON("container", "alpha"),
				pkgListEntryJSON("container", "beta"),
			})
		case strings.HasPrefix(r.URL.Path, "/orgs/"):
			hitOrgs = true
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, page, err := p.ListPackages(context.Background(), "o", provider.ListPackagesOptions{
		Type: "container",
	})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Type != "container" || got[0].Name != "alpha" || got[0].Owner != "o" {
		t.Errorf("first package: %+v", got[0])
	}
	if hitOrgs {
		t.Errorf("user owner shouldn't fall through to /orgs/")
	}
	if page == nil {
		t.Errorf("page should be non-nil")
	}
}

// TestListPackagesOrg routes /orgs/{owner}/packages when owner is an Organization.
func TestListPackagesOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("Organization"))
		case r.URL.Path == "/orgs/o/packages":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				pkgListEntryJSON("npm", "gamma"),
			})
		case r.URL.Path == "/users/o/packages":
			t.Errorf("Organization owner should not hit /users/.../packages")
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListPackages(context.Background(), "o", provider.ListPackagesOptions{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(got) != 1 || got[0].Name != "gamma" {
		t.Errorf("got %+v", got)
	}
}

func TestListPackagesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
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
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListPackages(context.Background(), "o", provider.ListPackagesOptions{})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth", got)
	}
}

// TestListPackagesPagination confirms per_page + page query encoding.
func TestListPackagesPagination(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("User"))
		case "/users/o/packages":
			capturedQuery = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, _, err := p.ListPackages(context.Background(), "o", provider.ListPackagesOptions{
		Type:  "npm",
		Limit: 5,
	}); err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	for _, want := range []string{"package_type=npm", "per_page=5"} {
		if !strings.Contains(capturedQuery, want) {
			t.Errorf("query %q missing %q", capturedQuery, want)
		}
	}
}

// TestGetPackageByVersionID resolves a numeric version directly as the
// GitHub version_id without listing versions first.
func TestGetPackageByVersionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("User"))
		case "/users/o/packages/container/alpha/versions/42":
			_ = json.NewEncoder(w).Encode(pkgVersionJSON(42, "sha256:abc"))
		case "/users/o/packages/container/alpha/versions":
			t.Errorf("numeric version should not list versions; got list call")
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPackage(context.Background(), "o", "container", "alpha", "42")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Type != "container" || got.Name != "alpha" || got.Owner != "o" {
		t.Errorf("got %+v", got)
	}
	// Version is the resolved name (not the input ID), so consumers see
	// the meaningful tag/sha rather than a numeric ID echoed back.
	if got.Version != "sha256:abc" {
		t.Errorf("version: got %q, want sha256:abc", got.Version)
	}
}

// TestGetPackageByVersionName resolves a non-numeric version by listing
// versions and matching against name OR container tags.
func TestGetPackageByVersionName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("User"))
		case "/users/o/packages/container/alpha/versions":
			// Two versions; one with a matching tag in metadata.container.tags.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         11,
					"name":       "sha256:zzz",
					"created_at": "2026-04-01T00:00:00Z",
					"metadata": map[string]any{
						"container": map[string]any{
							"tags": []string{"latest", "v1"},
						},
					},
				},
				{
					"id":         12,
					"name":       "sha256:aaa",
					"created_at": "2026-04-02T00:00:00Z",
					"metadata": map[string]any{
						"container": map[string]any{
							"tags": []string{"v2.0.0"},
						},
					},
				},
			})
		case "/users/o/packages/container/alpha/versions/12":
			_ = json.NewEncoder(w).Encode(pkgVersionJSON(12, "sha256:aaa"))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPackage(context.Background(), "o", "container", "alpha", "v2.0.0")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Version != "sha256:aaa" {
		t.Errorf("version: got %q, want sha256:aaa (resolved by tag)", got.Version)
	}
}

// TestGetPackageByVersionNameMatchesNameField resolves when the version
// string matches a version's `name` field directly (e.g., npm semver).
func TestGetPackageByVersionNameMatchesNameField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("User"))
		case "/users/o/packages/npm/pkg/versions":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 5, "name": "1.0.0", "created_at": "2026-04-01T00:00:00Z"},
				{"id": 6, "name": "1.1.0", "created_at": "2026-04-02T00:00:00Z"},
			})
		case "/users/o/packages/npm/pkg/versions/6":
			_ = json.NewEncoder(w).Encode(pkgVersionJSON(6, "1.1.0"))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPackage(context.Background(), "o", "npm", "pkg", "1.1.0")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Version != "1.1.0" {
		t.Errorf("version: got %q, want 1.1.0", got.Version)
	}
}

// TestGetPackageVersionNameNotFound returns NotFound when no version matches.
func TestGetPackageVersionNameNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("User"))
		case "/users/o/packages/npm/pkg/versions":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 5, "name": "1.0.0", "created_at": "2026-04-01T00:00:00Z"},
			})
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetPackage(context.Background(), "o", "npm", "pkg", "9.9.9")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound", got)
	}
}

func TestGetPackageNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetPackage(context.Background(), "o", "npm", "missing", "1.0.0")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound", got)
	}
}

func TestGetPackageAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetPackage(context.Background(), "o", "npm", "pkg", "1.0.0")
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth", got)
	}
}

// TestDeletePackageByVersionID deletes via numeric version_id directly.
func TestDeletePackageByVersionID(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("User"))
		default:
			if r.Method != http.MethodDelete {
				t.Errorf("method: %q", r.Method)
			}
			deletePath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeletePackage(context.Background(), "o", "container", "alpha", "42"); err != nil {
		t.Fatalf("DeletePackage: %v", err)
	}
	if deletePath != "/users/o/packages/container/alpha/versions/42" {
		t.Errorf("DELETE path: got %q", deletePath)
	}
}

// TestDeletePackageByVersionNameOrg ensures org-owners route through /orgs/.
func TestDeletePackageByVersionNameOrg(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/users/o":
			_ = json.NewEncoder(w).Encode(userTypeJSON("Organization"))
		case r.URL.Path == "/orgs/o/packages/npm/pkg/versions":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 99, "name": "2.0.0", "created_at": "2026-04-01T00:00:00Z"},
			})
		case r.Method == http.MethodDelete:
			deletePath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeletePackage(context.Background(), "o", "npm", "pkg", "2.0.0"); err != nil {
		t.Fatalf("DeletePackage: %v", err)
	}
	if deletePath != "/orgs/o/packages/npm/pkg/versions/99" {
		t.Errorf("DELETE path: got %q", deletePath)
	}
}

func TestDeletePackageNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeletePackage(context.Background(), "o", "npm", "missing", "1.0.0")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound", got)
	}
}

func TestDeletePackageAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeletePackage(context.Background(), "o", "npm", "pkg", "1.0.0")
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth", got)
	}
}

// TestUploadPackageNotImplemented pins the documented "not
// implemented" error from the GitHub upload stub. GitHub Packages
// publish flows are per-registry (npm publish, GHCR Docker v2 push,
// ...) and don't fold into one provider method; a follow-up issue
// covers per-kind dispatch. Until then, the error message is the
// contract — agents grep for "not implemented" + the pkgType to
// route around the gap.
func TestUploadPackageNotImplemented(t *testing.T) {
	p := github.NewProvider(github.Options{BaseURL: "https://example", Token: "X"})
	err := p.UploadPackage(
		context.Background(),
		"o", "container", "n", "v",
		provider.UploadPackageOptions{FileName: "f"},
		strings.NewReader("data"),
	)
	if err == nil {
		t.Fatal("UploadPackage should error on GitHub")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error: %q must mention 'not implemented'", err.Error())
	}
	// Per #324: NotImplemented stubs now surface the dedicated exit
	// code so agents can branch on "unsupported on this forge" vs
	// "transient failure" without parsing the message.
	if got := exitcode.Of(err); got != exitcode.NotImplemented {
		t.Errorf("exit code: got %d, want NotImplemented(12)", got)
	}
}
