package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestTagListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "v1.0.0", "commit": map[string]any{"sha": "abc"}, "message": "release one"},
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleTagList, map[string]any{"repo": "o/r"})
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
	if len(env.Data) != 1 || env.Data[0].Name != "v1.0.0" {
		t.Errorf("got %+v", env.Data)
	}
}

func TestTagCreateTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected %s", r.Method)
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "v2.0.0", "commit": map[string]any{"sha": "abc"}, "message": "",
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleTagCreate, map[string]any{
		"repo": "o/r", "name": "v2.0.0", "from": "main",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if data := envelopeData(t, res); data["name"] != "v2.0.0" {
		t.Errorf("name: %v", data["name"])
	}
}

func TestTagCreateToolRequiresName(t *testing.T) {
	res, _ := callTool(context.Background(), handleTagCreate, map[string]any{"repo": "o/r"})
	if res == nil || !res.IsError {
		t.Fatal("missing name: expected tool error")
	}
}

func TestTagDeleteTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected %s", r.Method)
		}
		w.WriteHeader(204)
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handleTagDelete, map[string]any{
		"repo": "o/r", "name": "v1.0.0",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if data := envelopeData(t, res); data["deleted"] != true {
		t.Errorf("deleted: %v", data["deleted"])
	}
}

func TestTagDeleteToolRequiresName(t *testing.T) {
	res, _ := callTool(context.Background(), handleTagDelete, map[string]any{"repo": "o/r"})
	if res == nil || !res.IsError {
		t.Fatal("missing name: expected tool error")
	}
}
