package aikestra

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

// The fixtures in testdata/fixtures.json are request/response exchanges
// recorded from Aikestra's real API handler by
// scripts/aikestra-record-fixtures.ts — not hand-written JSON. Re-record
// them when Aikestra's API changes.

const testToken = "test-token"

type exchange struct {
	Request struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	} `json:"request"`
	Response struct {
		Status          int             `json:"status"`
		WWWAuthenticate string          `json:"www_authenticate"`
		Body            json.RawMessage `json:"body"`
	} `json:"response"`
}

var (
	fixturesOnce sync.Once
	fixtures     map[string]exchange
)

func fixture(t *testing.T, name string) exchange {
	t.Helper()
	fixturesOnce.Do(func() {
		raw, err := os.ReadFile("testdata/fixtures.json")
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(raw, &fixtures); err != nil {
			panic(err)
		}
	})
	ex, ok := fixtures[name]
	if !ok {
		t.Fatalf("no fixture %q", name)
	}
	return ex
}

// replay starts a server that expects exactly the named exchanges, in
// order, and answers each with its recorded response. Any request that
// differs from the recording — method, path, query (order-insensitive),
// JSON body, or a missing bearer — fails the test, as does an exchange
// left unused.
func replay(t *testing.T, names ...string) *Provider {
	t.Helper()
	exchanges := make([]exchange, len(names))
	for i, n := range names {
		exchanges[i] = fixture(t, n)
	}
	var mu sync.Mutex
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if next >= len(exchanges) {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			http.Error(w, "unexpected", http.StatusTeapot)
			return
		}
		ex, name := exchanges[next], names[next]
		next++
		checkRequest(t, name, ex, r)
		if ex.Response.WWWAuthenticate != "" {
			w.Header().Set("WWW-Authenticate", ex.Response.WWWAuthenticate)
		}
		if len(ex.Response.Body) > 0 {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(ex.Response.Status)
		_, _ = w.Write(ex.Response.Body)
	}))
	t.Cleanup(func() {
		srv.Close()
		if next != len(exchanges) {
			t.Errorf("used %d of %d exchanges %v", next, len(exchanges), names)
		}
	})
	return NewProvider(Options{BaseURL: srv.URL + "/api/v1/", Token: testToken, RetryWait: time.Millisecond})
}

func checkRequest(t *testing.T, name string, ex exchange, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("%s: Authorization = %q", name, got)
	}
	want, err := url.Parse("/api/v1" + ex.Request.Path)
	if err != nil {
		t.Fatalf("%s: bad recorded path: %v", name, err)
	}
	if r.Method != ex.Request.Method || r.URL.Path != want.Path {
		t.Errorf("%s: got %s %s, recorded %s %s", name, r.Method, r.URL.Path, ex.Request.Method, want.Path)
	}
	if got, rec := r.URL.Query(), want.Query(); !reflect.DeepEqual(got, rec) {
		t.Errorf("%s: query %v, recorded %v", name, got, rec)
	}
	body, _ := io.ReadAll(r.Body)
	if len(ex.Request.Body) == 0 {
		if len(body) != 0 {
			t.Errorf("%s: sent body %s, recorded none", name, body)
		}
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s: Content-Type = %q", name, ct)
	}
	var got, rec any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Errorf("%s: body not JSON: %s", name, body)
	}
	_ = json.Unmarshal(ex.Request.Body, &rec)
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("%s: body %s, recorded %s", name, body, ex.Request.Body)
	}
}

// relationshipID is a relationship's recorded id, for asserting paths.
func relationshipID(t *testing.T, relation string, issue int) string {
	t.Helper()
	var list listResponse[apiRelationship]
	if err := json.Unmarshal(fixture(t, "relationships").Response.Body, &list); err != nil {
		t.Fatal(err)
	}
	for _, r := range list.Items {
		if r.Relation == relation && r.Issue == issue && r.ID != nil {
			return *r.ID
		}
	}
	t.Fatalf("no %s %d relationship recorded", relation, issue)
	return ""
}
