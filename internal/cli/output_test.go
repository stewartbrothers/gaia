package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestNDJSONRejectedOnSingleResource(t *testing.T) {
	var buf bytes.Buffer
	err := cli.RenderEnvelopeRejectsNDJSONForTest(&buf)
	if err == nil {
		t.Fatal("expected --format ndjson on single-resource to error")
	}
	if exitcode.Of(err) != exitcode.Usage {
		t.Errorf("expected Usage exit code, got %d (err: %v)", exitcode.Of(err), err)
	}
	if !strings.Contains(err.Error(), "single-resource") {
		t.Errorf("error should mention single-resource path: %v", err)
	}
}

func TestRenderListStreamingEmitsItemsAndTrailer(t *testing.T) {
	var buf bytes.Buffer
	fetch := cli.PageFetcherForTest(func(_ string) ([]any, *provider.Page, error) {
		return []any{
			map[string]any{"number": 1, "title": "first"},
			map[string]any{"number": 2, "title": "second"},
		}, &provider.Page{}, nil
	})
	if err := cli.RenderListStreamingForTest("ndjson", "", fetch, &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], `{"item":`) {
		t.Errorf("first line should be item-wrapped, got: %s", lines[0])
	}
	if !strings.Contains(lines[2], `"_metadata"`) {
		t.Errorf("last line should contain _metadata, got: %s", lines[2])
	}
	// Trailer reports total of 2.
	var trailer struct {
		Metadata struct {
			Total      int    `json:"total"`
			NextCursor string `json:"next_cursor"`
		} `json:"_metadata"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &trailer); err != nil {
		t.Fatalf("trailer unmarshal: %v", err)
	}
	if trailer.Metadata.Total != 2 {
		t.Errorf("trailer total: got %d, want 2", trailer.Metadata.Total)
	}
}

func TestRenderListStreamingPaginates(t *testing.T) {
	// Three pages of two items each, then a page with no items.
	pageCalls := 0
	fetch := cli.PageFetcherForTest(func(cursor string) ([]any, *provider.Page, error) {
		pageCalls++
		switch cursor {
		case "":
			return []any{"a", "b"}, &provider.Page{Truncated: true, NextCursor: "p2"}, nil
		case "p2":
			return []any{"c", "d"}, &provider.Page{Truncated: true, NextCursor: "p3"}, nil
		case "p3":
			return []any{"e", "f"}, &provider.Page{}, nil
		}
		t.Fatalf("unexpected cursor %q", cursor)
		return nil, nil, nil
	})
	var buf bytes.Buffer
	if err := cli.RenderListStreamingForTest("ndjson", "", fetch, &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if pageCalls != 3 {
		t.Errorf("expected 3 page fetches, got %d", pageCalls)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 7 {
		t.Errorf("expected 6 items + 1 trailer = 7 lines, got %d", len(lines))
	}
}

func TestRenderListStreamingExplicitCursorOnePage(t *testing.T) {
	// When the user passes --cursor, we fetch that one page and stop —
	// so the trailer surfaces next_cursor for them to resume.
	pageCalls := 0
	fetch := cli.PageFetcherForTest(func(_ string) ([]any, *provider.Page, error) {
		pageCalls++
		return []any{1, 2}, &provider.Page{Truncated: true, NextCursor: "next"}, nil
	})
	var buf bytes.Buffer
	if err := cli.RenderListStreamingForTest("ndjson", "p2", fetch, &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if pageCalls != 1 {
		t.Errorf("explicit cursor must fetch exactly one page; got %d", pageCalls)
	}
	out := buf.String()
	if !strings.Contains(out, `"next_cursor":"next"`) {
		t.Errorf("trailer should expose next_cursor=\"next\"; got: %s", out)
	}
}

func TestRenderListStreamingBrokenPipeStopsPagination(t *testing.T) {
	pageCalls := 0
	fetch := cli.PageFetcherForTest(func(_ string) ([]any, *provider.Page, error) {
		pageCalls++
		return []any{1, 2}, &provider.Page{Truncated: true, NextCursor: "p2"}, nil
	})
	if err := cli.RenderListStreamingForTest("ndjson", "", fetch, &brokenPipeWriter{}); err != nil {
		t.Errorf("broken pipe should be swallowed (consumer-cancellation); got %v", err)
	}
	if pageCalls != 1 {
		t.Errorf("expected exactly 1 page fetch before broken-pipe abort; got %d", pageCalls)
	}
}

func TestRenderListStreamingFallsThroughToJSON(t *testing.T) {
	fetch := cli.PageFetcherForTest(func(_ string) ([]any, *provider.Page, error) {
		return []any{map[string]any{"n": 1}}, &provider.Page{}, nil
	})
	var buf bytes.Buffer
	if err := cli.RenderListStreamingForTest("json", "", fetch, &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	// JSON path renders the standard envelope, not NDJSON.
	if !strings.Contains(buf.String(), `"schema_version"`) {
		t.Errorf("--format json should render envelope; got: %s", buf.String())
	}
	if strings.Contains(buf.String(), `"_metadata"`) {
		t.Errorf("--format json should not emit NDJSON trailer; got: %s", buf.String())
	}
}

// brokenPipeWriter returns io.ErrClosedPipe immediately, simulating
// a consumer that closed stdout after a partial read.
type brokenPipeWriter struct{}

func (b *brokenPipeWriter) Write(_ []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

// Reference errors so the import stays unconditionally needed even
// when test bodies above grow/shrink.
var _ = errors.Is
