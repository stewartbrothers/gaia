package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func mcpBPJSON(branch string, contexts []string, approvals int, strict bool) map[string]any {
	return map[string]any{
		"branch_name":              branch,
		"enable_status_check":      len(contexts) > 0,
		"status_check_contexts":    contexts,
		"required_approvals":       approvals,
		"block_on_outdated_branch": strict,
	}
}

func TestBranchListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "main", "commit": map[string]any{"id": "abc"}, "protected": true},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleBranchList, map[string]any{"repo": "o/r"})
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
	if len(env.Data) != 1 || env.Data[0].Name != "main" {
		t.Errorf("got %+v", env.Data)
	}
}

func TestBranchCreateTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected %s", r.Method)
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "feature/x", "commit": map[string]any{"id": "abc"}, "protected": false,
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleBranchCreate, map[string]any{
		"repo": "o/r", "name": "feature/x", "from": "main",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if data := envelopeData(t, res); data["name"] != "feature/x" {
		t.Errorf("name: %v", data["name"])
	}
}

func TestBranchCreateToolRequiresName(t *testing.T) {
	res, _ := callTool(context.Background(), handleBranchCreate, map[string]any{"repo": "o/r"})
	if res == nil || !res.IsError {
		t.Fatal("missing name: expected tool error")
	}
}

func TestBranchProtectionGetTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mcpBPJSON("main", []string{"CI / Build"}, 1, true))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleBranchProtectionGet, map[string]any{"repo": "o/r", "branch": "main"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	data := envelopeData(t, res)
	if data["branch"] != "main" {
		t.Errorf("branch: %v", data["branch"])
	}
}

func TestBranchProtectionGetToolRequiresBranch(t *testing.T) {
	res, _ := callTool(context.Background(), handleBranchProtectionGet, map[string]any{"repo": "o/r"})
	if res == nil || !res.IsError {
		t.Fatal("missing branch: expected tool error")
	}
}

func TestBranchProtectionSetTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(mcpBPJSON("main", []string{"CI / Build"}, 0, false))
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleBranchProtectionSet, map[string]any{
		"repo": "o/r", "branch": "main", "required_checks": []any{"CI / Build"},
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}
