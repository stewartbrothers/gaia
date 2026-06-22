package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestVariablesListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "TURBO_TEAM", "data": "acme", "created_at": "2026-05-05T12:02:18+10:00"},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleVariablesList, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	var env struct {
		Data []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(resultText(t, res)), &env); e != nil {
		t.Fatalf("decode: %v", e)
	}
	if len(env.Data) != 1 || env.Data[0].Name != "TURBO_TEAM" || env.Data[0].Value != "acme" {
		t.Errorf("got %+v", env.Data)
	}
}
