package github_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

const sampleDiff = `diff --git a/x b/x
index 1..2 100644
--- a/x
+++ b/x
@@ -1,1 +1,2 @@
 keep
+added
diff --git a/binary.png b/binary.png
index abc..def 100644
Binary files a/binary.png and b/binary.png differ
`

func TestGetPullRequestDiffRequestsCorrectAccept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github.v3.diff" {
			t.Errorf("Accept: got %q", got)
		}
		if r.URL.Path != "/repos/o/r/pulls/42" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(sampleDiff))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	files, err := p.GetPullRequestDiff(context.Background(), "o", "r", 42, provider.GetPullRequestDiffOptions{})
	if err != nil {
		t.Fatalf("GetPullRequestDiff: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("file count: %d", len(files))
	}
	if !files[1].Binary {
		t.Errorf("binary file flag missing: %+v", files[1])
	}
}

func TestGetPullRequestDiffPathsFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleDiff))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	files, err := p.GetPullRequestDiff(context.Background(), "o", "r", 42, provider.GetPullRequestDiffOptions{
		Paths: []string{"x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "x" {
		t.Errorf("filter: got %+v", files)
	}
}
