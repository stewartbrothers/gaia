package envelope_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/envelope"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func TestNewStampsSchemaVersion(t *testing.T) {
	e := envelope.New("hello")
	if e.SchemaVersion != types.SchemaVersion {
		t.Errorf("schema_version: got %q, want %q", e.SchemaVersion, types.SchemaVersion)
	}
	if e.Data != "hello" {
		t.Errorf("data: got %v, want %q", e.Data, "hello")
	}
}

func TestEnvelopeMarshalsBasicShape(t *testing.T) {
	e := envelope.New(map[string]any{"x": 1})
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)
	for _, want := range []string{`"schema_version"`, `"data"`} {
		if !strings.Contains(wire, want) {
			t.Errorf("missing %s in %s", want, wire)
		}
	}
	for _, banned := range []string{`"_truncated"`, `"_next_cursor"`, `"_meta"`} {
		if strings.Contains(wire, banned) {
			t.Errorf("unexpected %s in %s — should be omitempty when default", banned, wire)
		}
	}
}

func TestWithPageTruncated(t *testing.T) {
	e := envelope.New([]int{1, 2, 3}).WithPage(&provider.Page{Truncated: true, NextCursor: "abc"})
	if !e.Truncated || e.NextCursor != "abc" {
		t.Errorf("page fields: got truncated=%v next=%q", e.Truncated, e.NextCursor)
	}
	b, _ := json.Marshal(e)
	wire := string(b)
	if !strings.Contains(wire, `"_truncated":true`) || !strings.Contains(wire, `"_next_cursor":"abc"`) {
		t.Errorf("expected _truncated and _next_cursor on wire, got %s", wire)
	}
}

func TestWithPageNilIsNoOp(t *testing.T) {
	e := envelope.New([]int{1, 2, 3}).WithPage(nil)
	if e.Truncated || e.NextCursor != "" {
		t.Errorf("nil page should leave defaults; got truncated=%v next=%q", e.Truncated, e.NextCursor)
	}
}

func TestWithPageZeroValueIsNoTruncation(t *testing.T) {
	// A Provider that fits everything in one call returns &Page{}; the
	// envelope must NOT advertise truncation in that case.
	e := envelope.New([]int{1, 2}).WithPage(&provider.Page{})
	if e.Truncated || e.NextCursor != "" {
		t.Errorf("zero page should NOT show truncated; got %+v", e)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), `"_truncated"`) {
		t.Errorf("_truncated should be omitted for zero page; got %s", b)
	}
}

func TestWithMetaSetsKey(t *testing.T) {
	e := envelope.New("x").WithMeta("rate_remaining", 4988)
	if got := e.Meta["rate_remaining"]; got != 4988 {
		t.Errorf("meta key: got %v, want 4988", got)
	}
	b, _ := json.Marshal(e)
	if !strings.Contains(string(b), `"_meta":{"rate_remaining":4988}`) {
		t.Errorf("expected _meta on wire, got %s", b)
	}
}

func TestEnvelopeProjectAppliesFieldsToData(t *testing.T) {
	in := map[string]any{
		"number": 42,
		"title":  "hello",
		"author": map[string]any{"login": "alice", "extra": "drop"},
	}
	e := envelope.New(in)
	if err := e.Project("number,author.login"); err != nil {
		t.Fatalf("project: %v", err)
	}
	got, ok := e.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type after project: %T", e.Data)
	}
	if _, has := got["title"]; has {
		t.Errorf("title should have been dropped; got %+v", got)
	}
	author, ok := got["author"].(map[string]any)
	if !ok {
		t.Fatalf("author should remain a map; got %T", got["author"])
	}
	if _, has := author["extra"]; has {
		t.Errorf("author.extra should have been dropped; got %+v", author)
	}
}

func TestEnvelopeProjectPreservesEnvelopeFields(t *testing.T) {
	// Critical contract: --fields filters Data only, never the envelope's
	// own meta fields. Otherwise an agent that uses --fields would lose
	// schema_version / pagination state.
	e := envelope.New(map[string]any{"a": 1, "b": 2}).
		WithPage(&provider.Page{Truncated: true, NextCursor: "x"}).
		WithMeta("source", "forgejo")
	if err := e.Project("a"); err != nil {
		t.Fatalf("project: %v", err)
	}

	if e.SchemaVersion != types.SchemaVersion {
		t.Errorf("schema_version dropped after project")
	}
	if !e.Truncated || e.NextCursor != "x" {
		t.Errorf("page state lost after project: %+v", e)
	}
	if e.Meta["source"] != "forgejo" {
		t.Errorf("meta lost after project: %+v", e.Meta)
	}
}

func TestEnvelopeProjectEmptySpecIsNoOp(t *testing.T) {
	e := envelope.New(map[string]any{"a": 1, "b": 2})
	if err := e.Project(""); err != nil {
		t.Fatalf("project: %v", err)
	}
	got := e.Data.(map[string]any)
	if len(got) != 2 {
		t.Errorf("empty spec should leave data untouched; got %+v", got)
	}
}
