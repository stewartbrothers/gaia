package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRunnersListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "deploy-runner", "status": "online", "busy": false, "labels": []string{"self-hosted"}},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleRunnersList, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	var env struct {
		Data []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(resultText(t, res)), &env); e != nil {
		t.Fatalf("decode: %v", e)
	}
	if len(env.Data) != 1 || env.Data[0].Name != "deploy-runner" || env.Data[0].Status != "online" {
		t.Errorf("got %+v", env.Data)
	}
}
