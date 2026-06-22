package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollaboratorsListCLI(t *testing.T) {
	perms := map[string]string{"alice": "admin", "bob": "write"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/collaborators":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"login": "alice"},
				{"login": "bob"},
			})
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/collaborators/") &&
			strings.HasSuffix(r.URL.Path, "/permission"):
			parts := strings.Split(r.URL.Path, "/")
			login := parts[len(parts)-2]
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": perms[login]})
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "collaborators", "list")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var env struct {
		Data []struct {
			Login      string `json:"login"`
			Permission string `json:"permission"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(stdout), &env); e != nil {
		t.Fatalf("decode: %v\n%s", e, stdout)
	}
	if len(env.Data) != 2 || env.Data[0].Login != "alice" || env.Data[0].Permission != "admin" {
		t.Errorf("got %+v", env.Data)
	}
	if env.Data[1].Login != "bob" || env.Data[1].Permission != "write" {
		t.Errorf("got %+v", env.Data)
	}
}
