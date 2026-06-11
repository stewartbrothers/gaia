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
