package forgejo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

const diffSingleFileModified = `diff --git a/file.txt b/file.txt
index 1234567..89abcde 100644
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,3 @@
 line one
-line two
+line two modified
 line three
`

const diffMultipleFiles = `diff --git a/a.txt b/a.txt
index 111..222 100644
--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,3 @@
 a-line-one
+a-line-two
 a-line-three
diff --git a/b.txt b/b.txt
index 333..444 100644
--- a/b.txt
+++ b/b.txt
@@ -1,1 +1,1 @@
-old b
+new b
`

const diffMultipleHunks = `diff --git a/file.go b/file.go
index abc..def 100644
--- a/file.go
+++ b/file.go
@@ -10,3 +10,3 @@
 ten
-eleven
+ELEVEN
 twelve
@@ -50,2 +50,3 @@
 fifty
+inserted
 fifty-one
`

const diffNewFile = `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..abcdef0
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+content one
+content two
`

const diffDeletedFile = `diff --git a/old.txt b/old.txt
deleted file mode 100644
index abc1234..0000000
--- a/old.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-old line one
-old line two
`

const diffRenamed = `diff --git a/old.txt b/new.txt
similarity index 80%
rename from old.txt
rename to new.txt
index abc..def 100644
--- a/old.txt
+++ b/new.txt
@@ -1,3 +1,3 @@
 unchanged
-was old
+is new
 also unchanged
`

const diffBinary = `diff --git a/logo.png b/logo.png
index abc..def 100644
Binary files a/logo.png and b/logo.png differ
`

const diffMixed = `diff --git a/text.txt b/text.txt
index 111..222 100644
--- a/text.txt
+++ b/text.txt
@@ -1,1 +1,2 @@
 keep
+added
diff --git a/logo.png b/logo.png
index abc..def 100644
Binary files a/logo.png and b/logo.png differ
diff --git a/added.go b/added.go
new file mode 100644
index 0000000..abcdef0
--- /dev/null
+++ b/added.go
@@ -0,0 +1,1 @@
+package main
`

func TestParseDiffSingleFileModified(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff(diffSingleFileModified)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("file count: got %d, want 1", len(files))
	}
	f := files[0]
	if f.Path != "file.txt" {
		t.Errorf("path: got %q", f.Path)
	}
	if f.OldPath != "" {
		t.Errorf("OldPath should be empty for non-rename; got %q", f.OldPath)
	}
	if f.Status != "modified" {
		t.Errorf("status: got %q, want modified", f.Status)
	}
	if f.Binary {
		t.Errorf("Binary should be false")
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("hunks: got %d, want 1", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 3 {
		t.Errorf("hunk header: got %+v", h)
	}
	if len(h.Lines) != 4 {
		t.Fatalf("hunk lines: got %d, want 4", len(h.Lines))
	}
	if h.Lines[0] != " line one" || h.Lines[1] != "-line two" ||
		h.Lines[2] != "+line two modified" || h.Lines[3] != " line three" {
		t.Errorf("lines: got %+v", h.Lines)
	}
}

func TestParseDiffMultipleFiles(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff(diffMultipleFiles)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("file count: got %d, want 2", len(files))
	}
	if files[0].Path != "a.txt" || files[1].Path != "b.txt" {
		t.Errorf("paths: got %q, %q", files[0].Path, files[1].Path)
	}
	if files[0].Status != "modified" || files[1].Status != "modified" {
		t.Errorf("statuses: got %q, %q", files[0].Status, files[1].Status)
	}
}

func TestParseDiffMultipleHunks(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff(diffMultipleHunks)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("file count: got %d, want 1", len(files))
	}
	if len(files[0].Hunks) != 2 {
		t.Fatalf("hunk count: got %d, want 2", len(files[0].Hunks))
	}
	if files[0].Hunks[0].OldStart != 10 || files[0].Hunks[1].OldStart != 50 {
		t.Errorf("hunk starts: got %d, %d", files[0].Hunks[0].OldStart, files[0].Hunks[1].OldStart)
	}
}

func TestParseDiffNewFile(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff(diffNewFile)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatal("expected one file")
	}
	if files[0].Status != "added" {
		t.Errorf("status: got %q, want added", files[0].Status)
	}
	if files[0].Path != "new.txt" {
		t.Errorf("path: got %q", files[0].Path)
	}
	if len(files[0].Hunks) != 1 {
		t.Fatalf("hunks: got %d", len(files[0].Hunks))
	}
}

func TestParseDiffDeletedFile(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff(diffDeletedFile)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatal("expected one file")
	}
	if files[0].Status != "removed" {
		t.Errorf("status: got %q, want removed", files[0].Status)
	}
	if files[0].Path != "old.txt" {
		t.Errorf("path: got %q", files[0].Path)
	}
}

func TestParseDiffRenamed(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff(diffRenamed)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatal("expected one file")
	}
	f := files[0]
	if f.Status != "renamed" {
		t.Errorf("status: got %q, want renamed", f.Status)
	}
	if f.Path != "new.txt" {
		t.Errorf("Path: got %q, want new.txt", f.Path)
	}
	if f.OldPath != "old.txt" {
		t.Errorf("OldPath: got %q, want old.txt", f.OldPath)
	}
	if len(f.Hunks) != 1 {
		t.Errorf("rename with content change should keep hunks; got %d", len(f.Hunks))
	}
}

func TestParseDiffBinary(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff(diffBinary)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatal("expected one file")
	}
	if !files[0].Binary {
		t.Errorf("Binary should be true")
	}
	if len(files[0].Hunks) != 0 {
		t.Errorf("binary file should have no hunks; got %d", len(files[0].Hunks))
	}
	if files[0].Path != "logo.png" {
		t.Errorf("path: got %q", files[0].Path)
	}
}

func TestParseDiffMixed(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff(diffMixed)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("file count: got %d, want 3", len(files))
	}
	wantStatuses := []string{"modified", "modified", "added"}
	for i, want := range wantStatuses {
		if files[i].Status != want {
			t.Errorf("file[%d] status: got %q, want %q", i, files[i].Status, want)
		}
	}
	if !files[1].Binary {
		t.Errorf("middle file should be binary")
	}
}

func TestParseDiffEmpty(t *testing.T) {
	files, err := forgejo.ParseUnifiedDiff("")
	if err != nil {
		t.Fatalf("empty diff should not error; got %v", err)
	}
	if len(files) != 0 {
		t.Errorf("empty diff should produce no files; got %d", len(files))
	}
}

func TestGetPullRequestDiffEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/42.diff" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(diffMixed))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	files, err := p.GetPullRequestDiff(context.Background(), "o", "r", 42, provider.GetPullRequestDiffOptions{})
	if err != nil {
		t.Fatalf("GetPullRequestDiff: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("file count: got %d, want 3", len(files))
	}
}

func TestGetPullRequestDiffPathsFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(diffMultipleFiles))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	files, err := p.GetPullRequestDiff(context.Background(), "o", "r", 42, provider.GetPullRequestDiffOptions{
		Paths: []string{"a.txt"},
	})
	if err != nil {
		t.Fatalf("GetPullRequestDiff: %v", err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" {
		t.Errorf("paths filter: got %+v", files)
	}
}

func TestGetPullRequestDiffNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetPullRequestDiff(context.Background(), "o", "r", 999, provider.GetPullRequestDiffOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}
