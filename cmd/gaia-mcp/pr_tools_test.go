package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func makeMCPPRJSON(n int, state string) map[string]any {
	return map[string]any{
		"number": n, "title": "t", "state": state,
		"user":       map[string]any{"login": "a"},
		"head":       map[string]any{"ref": "feat", "sha": "abc", "repo": map[string]any{"full_name": "o/r"}},
		"base":       map[string]any{"ref": "main", "sha": "def", "repo": map[string]any{"full_name": "o/r"}},
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-02T00:00:00Z",
	}
}

func TestPRListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{makeMCPPRJSON(7, "open")})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRList, map[string]any{"repo": "o/r"})
	if err != nil {
		t.Fatal(err)
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Fatalf("count: %d", len(arr))
	}
}

func TestPRViewTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(makeMCPPRJSON(7, "open"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRView, map[string]any{"repo": "o/r", "number": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	d := envelopeData(t, res)
	if d["number"].(float64) != 7 {
		t.Errorf("got %+v", d)
	}
}

func TestPRDiffTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".diff") {
			t.Errorf("path: %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`diff --git a/x b/x
index 1..2 100644
--- a/x
+++ b/x
@@ -1,1 +1,2 @@
 keep
+added
`))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRDiff, map[string]any{"repo": "o/r", "number": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Errorf("file count: %d", len(arr))
	}
}

func TestPRCommentsTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "user": map[string]any{"login": "a"}, "body": "hi",
					"created_at": "2026-04-01T00:00:00Z",
					"updated_at": "2026-04-01T00:00:00Z"},
			})
		case "/repos/o/r/pulls/42/reviews", "/repos/o/r/pulls/42/comments":
			w.WriteHeader(404)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRComments, map[string]any{"repo": "o/r", "number": float64(42)})
	if err != nil {
		t.Fatal(err)
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Fatalf("count: %d", len(arr))
	}
}

func TestPRCreateTool(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(makeMCPPRJSON(99, "open"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRCreate, map[string]any{
		"repo": "o/r", "title": "feat", "head": "feature/x", "base": "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal(resultText(t, res))
	}
	if !strings.Contains(string(captured), `"head":"feature/x"`) {
		t.Errorf("captured: %s", captured)
	}
}

func TestPRCreateMissingFieldsRejected(t *testing.T) {
	for _, args := range []map[string]any{
		{"repo": "o/r"},                            // no title/head/base
		{"repo": "o/r", "title": "x"},              // no head/base
		{"repo": "o/r", "title": "x", "head": "y"}, // no base
	} {
		res, _ := callTool(context.Background(), handlePRCreate, args)
		if !res.IsError {
			t.Errorf("expected error for %+v", args)
		}
	}
}

func TestPREditDraftFlag(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(makeMCPPRJSON(7, "open"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePREdit, map[string]any{
		"repo": "o/r", "number": float64(7), "draft": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if !strings.Contains(string(captured), `"draft":true`) {
		t.Errorf("draft not sent: %s", captured)
	}
}

func TestPRMergeTool(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRMerge, map[string]any{
		"repo": "o/r", "number": float64(7), "method": "squash",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if !strings.Contains(string(captured), `"do":"squash"`) {
		t.Errorf("method: %s", captured)
	}
}

func TestPRReviewToolWithInline(t *testing.T) {
	var captured []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRReview, map[string]any{
		"repo":   "o/r",
		"number": float64(7),
		"state":  "request-changes",
		"body":   "see inline",
		"comments": []any{
			map[string]any{"path": "x.go", "line": float64(10), "body": "rename"},
			map[string]any{"path": "y.go", "line": float64(20), "body": "tighten"},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	var got map[string]any
	_ = json.Unmarshal(captured, &got)
	if got["event"] != "REQUEST_CHANGES" {
		t.Errorf("event: %+v", got)
	}
	if comments, _ := got["comments"].([]any); len(comments) != 2 {
		t.Errorf("comments count: %d", len(comments))
	}
}

func TestPRReviewBadInlineFormat(t *testing.T) {
	res, _ := callTool(context.Background(), handlePRReview, map[string]any{
		"repo": "o/r", "number": float64(7), "state": "approve",
		"comments": []any{map[string]any{"path": "x.go"}}, // missing line + body
	})
	if !res.IsError {
		t.Errorf("expected error for bad inline shape")
	}
}

// --- B-3 / #112: gaia_pr_ci_wait MCP tool ---

// makeMCPCIStatus builds a Forgejo /commits/{sha}/status payload for
// the CI-wait MCP tool tests.
func makeMCPCIStatus(rollup string, items []map[string]string) map[string]any {
	statuses := make([]map[string]any, 0, len(items))
	for _, it := range items {
		statuses = append(statuses, map[string]any{
			"status":  it["state"], // Forgejo API field is "status", not "state"
			"context": it["name"],
		})
	}
	return map[string]any{"state": rollup, "statuses": statuses}
}

func TestPRCIWaitToolSuccess(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(makeMCPPRJSON(7, "open"))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(makeMCPCIStatus("success",
				[]map[string]string{{"name": "ci/build", "state": "success"}}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRCIWait, map[string]any{
		"repo": "o/r", "number": float64(7),
		"timeout": "1s", "interval": "10ms",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	d := envelopeData(t, res)
	if d["outcome"] != "success" {
		t.Errorf("outcome: got %v want success", d["outcome"])
	}
	if d["exit_code"].(float64) != 0 {
		t.Errorf("exit_code: got %v want 0", d["exit_code"])
	}
}

func TestPRCIWaitToolCheckFailed(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(makeMCPPRJSON(7, "open"))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(makeMCPCIStatus("failure",
				[]map[string]string{
					{"name": "ci/build", "state": "success"},
					{"name": "ci/test", "state": "failure"},
				}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRCIWait, map[string]any{
		"repo": "o/r", "number": float64(7),
		"timeout": "1s", "interval": "10ms",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	d := envelopeData(t, res)
	if d["outcome"] != "check_failed" {
		t.Errorf("outcome: got %v want check_failed", d["outcome"])
	}
	if d["exit_code"].(float64) != 10 {
		t.Errorf("exit_code: got %v want 10", d["exit_code"])
	}
}

func TestPRCIWaitToolCheckFlakyOnTimeout(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(makeMCPPRJSON(7, "open"))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(makeMCPCIStatus("pending",
				[]map[string]string{{"name": "ci/build", "state": "pending"}}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePRCIWait, map[string]any{
		"repo": "o/r", "number": float64(7),
		"timeout": "30ms", "interval": "10ms",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	d := envelopeData(t, res)
	if d["outcome"] != "check_flaky" {
		t.Errorf("outcome: got %v want check_flaky", d["outcome"])
	}
	if d["exit_code"].(float64) != 11 {
		t.Errorf("exit_code: got %v want 11", d["exit_code"])
	}
}

func TestPRCIWaitToolFlakyMarkers(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(makeMCPPRJSON(7, "open"))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(makeMCPCIStatus("failure",
				[]map[string]string{{"name": "e2e-canary", "state": "failure"}}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	})
	pinBuilder(t, p)

	// Without marker → CheckFailed.
	res, _ := callTool(context.Background(), handlePRCIWait, map[string]any{
		"repo": "o/r", "number": float64(7),
		"timeout": "1s", "interval": "10ms",
	})
	d := envelopeData(t, res)
	if d["outcome"] != "check_failed" {
		t.Errorf("baseline: got %v want check_failed", d["outcome"])
	}

	// With "canary" added → CheckFlaky.
	res, _ = callTool(context.Background(), handlePRCIWait, map[string]any{
		"repo": "o/r", "number": float64(7),
		"timeout": "1s", "interval": "10ms",
		"flaky_markers": []any{"canary"},
	})
	d = envelopeData(t, res)
	if d["outcome"] != "check_flaky" {
		t.Errorf("with marker: got %v want check_flaky", d["outcome"])
	}
}

func TestPRCIWaitToolMissingNumber(t *testing.T) {
	res, _ := callTool(context.Background(), handlePRCIWait, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Errorf("expected error when number missing")
	}
}

func TestPRCIWaitToolBadDuration(t *testing.T) {
	res, _ := callTool(context.Background(), handlePRCIWait, map[string]any{
		"repo": "o/r", "number": float64(7),
		"timeout": "not-a-duration",
	})
	if !res.IsError {
		t.Errorf("expected error on bad timeout")
	}
}
