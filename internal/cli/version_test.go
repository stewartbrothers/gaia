package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	var env struct {
		SchemaVersion string `json:"schema_version"`
		Data          struct {
			Version   string `json:"version"`
			Commit    string `json:"commit"`
			GoVersion string `json:"go_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if env.SchemaVersion == "" {
		t.Errorf("schema_version empty")
	}
	if env.Data.Version == "" {
		t.Errorf("version empty")
	}
	if env.Data.GoVersion == "" {
		t.Errorf("go_version empty")
	}
}

func TestVersionPretty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "pretty", "version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.HasPrefix(out, "gaia ") {
		t.Errorf("pretty output should start with 'gaia '; got %q", out)
	}
	if !strings.Contains(out, "go") {
		t.Errorf("pretty output should mention go version; got %q", out)
	}
}
