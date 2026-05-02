package envelope_test

import (
	"reflect"
	"testing"

	"github.com/stewartbrothers/gaia/core/envelope"
)

func TestParseFieldsEmpty(t *testing.T) {
	if got := envelope.ParseFields(""); len(got) != 0 {
		t.Errorf("empty spec should parse to empty FieldSpec; got %+v", got)
	}
	if got := envelope.ParseFields("   "); len(got) != 0 {
		t.Errorf("whitespace-only spec should parse to empty FieldSpec; got %+v", got)
	}
}

func TestParseFieldsFlat(t *testing.T) {
	got := envelope.ParseFields("a,b,c")
	want := envelope.FieldSpec{"a": {}, "b": {}, "c": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseFieldsDotted(t *testing.T) {
	got := envelope.ParseFields("a.b,c.d.e")
	want := envelope.FieldSpec{
		"a": {"b": {}},
		"c": {"d": {"e": {}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseFieldsTrimsAndIgnoresEmpty(t *testing.T) {
	got := envelope.ParseFields(" a , , b ")
	want := envelope.FieldSpec{"a": {}, "b": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestApplyKeepsListedTopLevelKeys(t *testing.T) {
	in := map[string]any{"a": 1, "b": 2, "c": 3}
	got := envelope.ParseFields("a,c").Apply(in)
	want := map[string]any{"a": 1, "c": 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestApplyDropsUnknownKeysSilently(t *testing.T) {
	in := map[string]any{"a": 1}
	got := envelope.ParseFields("a,nonexistent").Apply(in)
	want := map[string]any{"a": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestApplyDescendsIntoMaps(t *testing.T) {
	in := map[string]any{
		"author": map[string]any{"login": "alice", "name": "Alice", "id": 7},
		"title":  "hi",
	}
	got := envelope.ParseFields("author.login,title").Apply(in)
	want := map[string]any{
		"author": map[string]any{"login": "alice"},
		"title":  "hi",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestApplyDescendsIntoArrays(t *testing.T) {
	in := map[string]any{
		"labels": []any{
			map[string]any{"name": "bug", "color": "red"},
			map[string]any{"name": "p1", "color": "blue"},
		},
	}
	got := envelope.ParseFields("labels.name").Apply(in)
	want := map[string]any{
		"labels": []any{
			map[string]any{"name": "bug"},
			map[string]any{"name": "p1"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestApplyTopLevelArray(t *testing.T) {
	in := []any{
		map[string]any{"a": 1, "b": 2},
		map[string]any{"a": 3, "b": 4},
	}
	got := envelope.ParseFields("a").Apply(in)
	want := []any{
		map[string]any{"a": 1},
		map[string]any{"a": 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestApplyEmptySpecReturnsInputUnchanged(t *testing.T) {
	in := map[string]any{"a": 1, "b": 2}
	got := envelope.FieldSpec{}.Apply(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("empty spec should be identity; got %+v", got)
	}
}

func TestApplyDescendingThroughScalarKeepsScalar(t *testing.T) {
	// Best-effort: "a.b" against {a: 5} keeps {a: 5} rather than dropping
	// or erroring. Documented behavior — agents see the data they have.
	in := map[string]any{"a": 5}
	got := envelope.ParseFields("a.b").Apply(in)
	want := map[string]any{"a": 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestApplyHandlesNil(t *testing.T) {
	got := envelope.ParseFields("a").Apply(nil)
	if got != nil {
		t.Errorf("nil input should yield nil; got %+v", got)
	}
}
