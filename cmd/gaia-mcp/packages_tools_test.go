package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
)

func packageJSON(typ, name, version, owner string) map[string]any {
	return map[string]any{
		"type":       typ,
		"name":       name,
		"version":    version,
		"owner":      map[string]any{"login": owner},
		"created_at": "2026-04-01T00:00:00Z",
	}
}

func TestPackagesListTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages/o" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			packageJSON("generic", "alpha", "1.0.0", "o"),
			packageJSON("generic", "beta", "0.2.0", "o"),
		})
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePackagesList, map[string]any{"owner": "o"})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	arr := envelopeSlice(t, res)
	if len(arr) != 2 {
		t.Fatalf("count: %d", len(arr))
	}
}

func TestPackagesListToolRequiresOwner(t *testing.T) {
	res, _ := callTool(context.Background(), handlePackagesList, map[string]any{})
	if !res.IsError {
		t.Error("missing owner must error")
	}
}

func TestPackagesViewTool(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages/o/generic/alpha/1.0.0" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(packageJSON("generic", "alpha", "1.0.0", "o"))
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePackagesView, map[string]any{
		"owner":   "o",
		"type":    "generic",
		"name":    "alpha",
		"version": "1.0.0",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
}

func TestPackagesViewToolRequiresFields(t *testing.T) {
	cases := []map[string]any{
		{},                                // missing all
		{"owner": "o"},                    // missing type+name+version
		{"owner": "o", "type": "generic"}, // missing name+version
		{"owner": "o", "type": "generic", "name": "alpha"},       // missing version
		{"owner": "o", "type": "generic", "version": "1.0.0"},    // missing name
		{"owner": "o", "name": "alpha", "version": "1.0.0"},      // missing type
		{"type": "generic", "name": "alpha", "version": "1.0.0"}, // missing owner
	}
	for i, args := range cases {
		res, _ := callTool(context.Background(), handlePackagesView, args)
		if !res.IsError {
			t.Errorf("case %d: missing field should error; args=%v", i, args)
		}
	}
}

func TestPackagesDeletePreview(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(_ http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteHits, 1)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePackagesDelete, map[string]any{
		"owner":   "o",
		"type":    "generic",
		"name":    "alpha",
		"version": "1.0.0",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 0 {
		t.Errorf("preview must not DELETE; got %d", deleteHits)
	}
}

func TestPackagesDeleteWithConfirm(t *testing.T) {
	deleteHits := int32(0)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteHits, 1)
			w.WriteHeader(204)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePackagesDelete, map[string]any{
		"owner":   "o",
		"type":    "generic",
		"name":    "alpha",
		"version": "1.0.0",
		"confirm": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if atomic.LoadInt32(&deleteHits) != 1 {
		t.Errorf("confirm=true must DELETE; got %d", deleteHits)
	}
}
