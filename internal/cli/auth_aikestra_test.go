package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestAuthAikestraHappyPath(t *testing.T) {
	clearGaiaEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user" {
			t.Errorf("path: got %q, want /api/v1/user", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer PASTED" {
			t.Errorf("token: got %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"principal":"delegated-agent","credential_id":"c1","id":"u1","email":"ada@example.com","name":"ada"}`))
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("PASTED\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "aikestra", srv.URL + "/"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "ada@example.com") || !strings.Contains(out, "Access tokens") {
		t.Errorf("output: %q", out)
	}

	credPath := filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml")
	store, err := auth.Load(credPath)
	if err != nil {
		t.Fatalf("Load credentials: %v", err)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	c, ok := store.Get("aikestra", host)
	if !ok {
		t.Fatalf("expected credential for aikestra:%s; got hosts=%v", host, store.Hosts())
	}
	if c.Token != "PASTED" || c.User != "ada@example.com" || c.APIURL != srv.URL+"/api/v1" {
		t.Errorf("credential: got %+v", c)
	}
}

func TestAuthAikestraInvalidTokenIsNotPersisted(t *testing.T) {
	clearGaiaEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="aikestra"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"That access token is not valid"}}`))
	}))
	defer srv.Close()

	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("BAD\n"))
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"auth", "aikestra", srv.URL + "/api/v1"})

	err := root.Execute()
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d (%v), want Auth(4)", got, err)
	}
	credPath := filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml")
	if _, err := os.Stat(credPath); err == nil {
		t.Errorf("credentials file should not exist after invalid token: %s", credPath)
	}
}
