package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSecretsListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "GH_RELEASE_TOKEN", "created_at": "2026-05-05T12:02:18+10:00"},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleSecretsList, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	var env struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(resultText(t, res)), &env); e != nil {
		t.Fatalf("decode: %v", e)
	}
	if len(env.Data) != 1 || env.Data[0].Name != "GH_RELEASE_TOKEN" {
		t.Errorf("got %+v", env.Data)
	}
}
