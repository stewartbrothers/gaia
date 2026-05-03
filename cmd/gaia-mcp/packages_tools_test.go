package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
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

// TestPackagesUploadToolBase64 covers the typical MCP binary path:
// the agent base64-encodes the artifact, the tool decodes and PUTs.
func TestPackagesUploadToolBase64(t *testing.T) {
	var (
		gotPath string
		gotBody []byte
	)
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			gotPath = r.URL.Path
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(201)
		}
	})
	pinBuilder(t, p)

	payload := []byte("BINARY-PAYLOAD-VIA-BASE64")
	res, err := callTool(context.Background(), handlePackagesUpload, map[string]any{
		"owner":       "o",
		"type":        "generic",
		"name":        "myapp",
		"version":     "1.2.0",
		"filename":    "release.tar.gz",
		"body_base64": base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if gotPath != "/packages/o/generic/myapp/1.2.0/release.tar.gz" {
		t.Errorf("path: got %q", gotPath)
	}
	if string(gotBody) != string(payload) {
		t.Errorf("body: got %q", string(gotBody))
	}
}

// TestPackagesUploadToolBodyText covers the convenience path for
// text-shaped artifacts (the body is passed inline, no base64).
func TestPackagesUploadToolBodyText(t *testing.T) {
	var gotBody []byte
	p, _ := fakeForgeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(201)
		}
	})
	pinBuilder(t, p)

	res, err := callTool(context.Background(), handlePackagesUpload, map[string]any{
		"owner":    "o",
		"type":     "generic",
		"name":     "x",
		"version":  "1",
		"filename": "f.txt",
		"body":     "TEXT-PAYLOAD",
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, resultText(t, res))
	}
	if string(gotBody) != "TEXT-PAYLOAD" {
		t.Errorf("body: got %q", string(gotBody))
	}
}

// TestPackagesUploadToolRequiresBody asserts the empty-body case
// surfaces a usage error rather than uploading zero bytes silently.
func TestPackagesUploadToolRequiresBody(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("missing body must not reach the upstream")
	})
	pinBuilder(t, p)

	res, _ := callTool(context.Background(), handlePackagesUpload, map[string]any{
		"owner":    "o",
		"type":     "generic",
		"name":     "x",
		"version":  "1",
		"filename": "f",
	})
	if !res.IsError {
		t.Error("missing body must error")
	}
}

// TestPackagesUploadToolRejectsBothBodies pins the mutually-exclusive
// rule so callers don't get an ambiguous "which one wins" surprise.
func TestPackagesUploadToolRejectsBothBodies(t *testing.T) {
	p, _ := fakeForgeProvider(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("conflicting body args must not reach the upstream")
	})
	pinBuilder(t, p)

	res, _ := callTool(context.Background(), handlePackagesUpload, map[string]any{
		"owner":       "o",
		"type":        "generic",
		"name":        "x",
		"version":     "1",
		"filename":    "f",
		"body":        "a",
		"body_base64": base64.StdEncoding.EncodeToString([]byte("b")),
	})
	if !res.IsError {
		t.Error("both body and body_base64 must error")
	}
}

// TestPackagesUploadToolRequiresFileName: filename empty surfaces a
// usage error.
func TestPackagesUploadToolRequiresFileName(t *testing.T) {
	res, _ := callTool(context.Background(), handlePackagesUpload, map[string]any{
		"owner":   "o",
		"type":    "generic",
		"name":    "x",
		"version": "1",
		"body":    "data",
	})
	if !res.IsError {
		t.Error("missing filename must error")
	}
}

// TestPackagesUploadToolBadBase64 surfaces a decode error rather than
// uploading garbage.
func TestPackagesUploadToolBadBase64(t *testing.T) {
	res, _ := callTool(context.Background(), handlePackagesUpload, map[string]any{
		"owner":       "o",
		"type":        "generic",
		"name":        "x",
		"version":     "1",
		"filename":    "f",
		"body_base64": "this-is-not-base64!!!",
	})
	if !res.IsError {
		t.Error("malformed base64 must error")
	}
}
