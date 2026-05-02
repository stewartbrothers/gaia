package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSearchTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "leak" {
			t.Errorf("query: %q", r.URL.Query().Get("q"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"number": 42, "title": "memory leak", "repository": map[string]any{"full_name": "o/r"}},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleSearch, map[string]any{
		"query": "leak", "repo": "o/r",
	})
	if err != nil {
		t.Fatal(err)
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 1 {
		t.Fatalf("count: %d", len(arr))
	}
}

func TestSearchEmptyQueryRejected(t *testing.T) {
	res, _ := callTool(context.Background(), handleSearch, map[string]any{})
	if !res.IsError {
		t.Errorf("expected IsError")
	}
}
