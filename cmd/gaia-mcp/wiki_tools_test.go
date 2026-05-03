package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// wikiMetaJSON returns the on-the-wire shape Forgejo's wiki/pages
// list endpoint emits per page (no body).
func wikiMetaJSON(title, sha string) map[string]any {
	return map[string]any{
		"title":   title,
		"sub_url": title,
		"last_commit": map[string]any{
			"sha": sha,
			"commiter": map[string]any{
				"name": "alice", "email": "alice@example",
				"date": "2026-04-01T00:00:00Z",
			},
		},
	}
}

// wikiPageFullJSON returns the full GET /wiki/page/{slug} shape.
func wikiPageFullJSON(title, body, sha string) map[string]any {
	return map[string]any{
		"title":          title,
		"sub_url":        title,
		"content_base64": base64.StdEncoding.EncodeToString([]byte(body)),
		"last_commit": map[string]any{
			"sha": sha,
			"commiter": map[string]any{
				"name": "alice", "email": "alice@example",
				"date": "2026-05-03T00:00:00Z",
			},
		},
	}
}

func TestWikiListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			wikiMetaJSON("Home", "deadbeef1234567"),
			wikiMetaJSON("Setup", "cafebabe9876543"),
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWikiList, map[string]any{"repo": "o/r"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 2 {
		t.Fatalf("count: %d", len(arr))
	}
}

func TestWikiViewToolRequiresPath(t *testing.T) {
	res, _ := callTool(context.Background(), handleWikiView, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing path must error")
	}
}

func TestWikiViewTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/wiki/page/Home" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(wikiPageFullJSON("Home", "# body", "abc1234"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWikiView, map[string]any{
		"repo": "o/r", "path": "Home",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	// WikiPage.Title now serialises as a trust-tagged object
	// (#146). The wire shape is `"title":{"_trust":"external",
	// "_value":"Home"}` rather than a plain string.
	if !strings.Contains(resultText(t, res), `"title":{"_trust":"external","_value":"Home"}`) {
		t.Errorf("expected trust-tagged Home title in result; got %q", resultText(t, res))
	}
}

func TestWikiSearchTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/wiki/pages":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				wikiMetaJSON("Home", "sha1234567"),
				wikiMetaJSON("Setup", "sha2345678"),
			})
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/wiki/page/"):
			title := strings.TrimPrefix(r.URL.Path, "/repos/o/r/wiki/page/")
			body := "this page mentions FOO once"
			if title == "Setup" {
				body = "totally unrelated content"
			}
			_ = json.NewEncoder(w).Encode(wikiPageFullJSON(title, body, "sha"))
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWikiSearch, map[string]any{
		"repo": "o/r", "query": "FOO",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Errorf("expected 1 hit; got %d (%s)", len(arr), resultText(t, res))
	}
}

func TestWikiSearchToolRequiresQuery(t *testing.T) {
	res, _ := callTool(context.Background(), handleWikiSearch, map[string]any{"repo": "o/r"})
	if !res.IsError {
		t.Error("missing query must error")
	}
}

func TestWikiEditToolRequiresPathAndBody(t *testing.T) {
	for _, args := range []map[string]any{
		{"repo": "o/r", "body": "x"},
		{"repo": "o/r", "path": "Home"},
	} {
		res, _ := callTool(context.Background(), handleWikiEdit, args)
		if !res.IsError {
			t.Errorf("expected error for args=%v", args)
		}
	}
}

func TestWikiEditToolUpserts(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(404) // does not exist yet
		case http.MethodPost:
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(wikiPageFullJSON("Home", "new body", "newsha1234567"))
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWikiEdit, map[string]any{
		"repo": "o/r", "path": "Home", "body": "new body",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestWikiDeleteToolPreview(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(_ http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteHits, 1)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWikiDelete, map[string]any{
		"repo": "o/r", "path": "Home",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 0 {
		t.Errorf("preview must not DELETE; got %d", deleteHits)
	}
}

func TestWikiDeleteToolWithConfirm(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteHits, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleWikiDelete, map[string]any{
		"repo": "o/r", "path": "Home", "confirm": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 1 {
		t.Errorf("confirm=true must DELETE; got %d", deleteHits)
	}
}
