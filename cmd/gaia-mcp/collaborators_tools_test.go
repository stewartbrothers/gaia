package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCollaboratorsListTool(t *testing.T) {
	perms := map[string]string{"alice": "admin"}
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/collaborators":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"login": "alice"}})
		case strings.HasSuffix(r.URL.Path, "/permission"):
			parts := strings.Split(r.URL.Path, "/")
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": perms[parts[len(parts)-2]]})
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleCollaboratorsList, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	var env struct {
		Data []struct {
			Login      string `json:"login"`
			Permission string `json:"permission"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(resultText(t, res)), &env); e != nil {
		t.Fatalf("decode: %v", e)
	}
	if len(env.Data) != 1 || env.Data[0].Login != "alice" || env.Data[0].Permission != "admin" {
		t.Errorf("got %+v", env.Data)
	}
}
