package chain_test

import (
	"reflect"
	"testing"

	"github.com/stewartbrothers/gaia/core/chain"
)

func TestSubstituteVars(t *testing.T) {
	scope := chain.Scope{
		Vars: map[string]string{"title": "feat: thing", "base": "main"},
	}
	got, unresolved := chain.Substitute(`gaia pr create --title "${title}" --base ${base}`, scope)
	want := `gaia pr create --title "feat: thing" --base main`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if len(unresolved) != 0 {
		t.Errorf("unresolved: %+v", unresolved)
	}
}

func TestSubstituteCaptureField(t *testing.T) {
	scope := chain.Scope{
		Captures: map[string]any{
			"pr": map[string]any{"number": float64(42), "title": "x"},
		},
	}
	got, _ := chain.Substitute(`gaia pr merge ${pr.number} --method squash`, scope)
	want := `gaia pr merge 42 --method squash`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteCaptureWholeObject(t *testing.T) {
	scope := chain.Scope{
		Captures: map[string]any{
			"pr": map[string]any{"number": float64(42), "title": "x"},
		},
	}
	got, _ := chain.Substitute(`echo '${pr}'`, scope)
	// JSON output — keys may sort differently across Go versions,
	// just verify both fields are present.
	if !contains(got, `"number":42`) || !contains(got, `"title":"x"`) {
		t.Errorf("got %q (expected JSON with number+title)", got)
	}
}

func TestSubstituteCaptureNested(t *testing.T) {
	scope := chain.Scope{
		Captures: map[string]any{
			"pr": map[string]any{
				"head": map[string]any{"ref": "feature/x", "sha": "abc"},
			},
		},
	}
	got, _ := chain.Substitute(`branch ${pr.head.ref} sha ${pr.head.sha}`, scope)
	want := `branch feature/x sha abc`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteUnresolvedLeavesPlaceholder(t *testing.T) {
	got, unresolved := chain.Substitute(`hello ${name} ${pr.number}`, chain.Scope{})
	if got != `hello ${name} ${pr.number}` {
		t.Errorf("placeholder must be left literal; got %q", got)
	}
	want := []string{"name", "pr.number"}
	if !reflect.DeepEqual(unresolved, want) {
		t.Errorf("unresolved: got %+v, want %+v", unresolved, want)
	}
}

func TestSubstituteVarBeatsCaptureForBareName(t *testing.T) {
	// `${pr}` (no dot) finds Vars["pr"] first when both exist.
	scope := chain.Scope{
		Vars: map[string]string{"pr": "from-vars"},
		Captures: map[string]any{
			"pr": map[string]any{"number": float64(1)},
		},
	}
	got, _ := chain.Substitute(`${pr}`, scope)
	if got != "from-vars" {
		t.Errorf("var should win for bare ${pr}; got %q", got)
	}
}

func TestSubstituteEmptyAndMalformedRefs(t *testing.T) {
	scope := chain.Scope{Vars: map[string]string{"x": "y"}}
	cases := []struct {
		in   string
		want string
	}{
		{"plain text", "plain text"},
		{"${unterminated", "${unterminated"},
		{"${}", "${}"},                 // empty ref unresolvable
		{"$${literal}", "$${literal}"}, // first $ then ${literal} — literal unresolved
		{"${x}-${x}", "y-y"},           // multiple refs same line
		{"${x.}", "${x.}"},             // trailing dot rejected
		{"${.x}", "${.x}"},             // leading dot rejected
		{"${1bad}", "${1bad}"},         // ident can't start with digit
	}
	for _, tc := range cases {
		got, _ := chain.Substitute(tc.in, scope)
		if got != tc.want {
			t.Errorf("Substitute(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSubstituteScalarTypes(t *testing.T) {
	scope := chain.Scope{
		Captures: map[string]any{
			"step": map[string]any{
				"int":   float64(123),
				"float": 1.5,
				"bool":  true,
				"null":  nil,
				"str":   "literal",
			},
		},
	}
	cases := []struct{ ref, want string }{
		{"${step.int}", "123"},
		{"${step.float}", "1.5"},
		{"${step.bool}", "true"},
		{"${step.null}", ""},
		{"${step.str}", "literal"},
	}
	for _, tc := range cases {
		got, _ := chain.Substitute(tc.ref, scope)
		if got != tc.want {
			t.Errorf("Substitute(%q): got %q, want %q", tc.ref, got, tc.want)
		}
	}
}

// helper

func contains(s, sub string) bool {
	return len(s) >= len(sub) && stringIndex(s, sub) >= 0
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
