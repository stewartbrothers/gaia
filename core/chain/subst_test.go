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

// Shell-quoting regression tests (#135).
//
// SubstituteShell is the variant the chain runner uses for run-line
// substitution. Substituted values must be wrapped in POSIX
// single-quote literals so a hostile var/capture can't inject shell
// metacharacters that change the surrounding command's shape.

func TestSubstituteShellQuotesHostilePayload(t *testing.T) {
	scope := chain.Scope{
		Vars: map[string]string{"name": `'; rm -rf / #`},
	}
	got, unresolved := chain.SubstituteShell(`echo Hello, ${name}`, scope)
	want := `echo Hello, ''\''; rm -rf / #'`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if len(unresolved) != 0 {
		t.Errorf("unresolved: %+v", unresolved)
	}
}

func TestSubstituteShellQuotesAllShellMetachars(t *testing.T) {
	// A capture object containing every nasty character a hostile
	// forge response might include. None of these should escape the
	// single-quoted literal — backticks, `$()`, `;`, `&`, `|`,
	// newline, single-quote.
	hostile := "before`whoami`mid$(id);x&y|z\n'after"
	scope := chain.Scope{
		Captures: map[string]any{
			"obj": map[string]any{"field": hostile},
		},
	}
	got, _ := chain.SubstituteShell(`echo ${obj.field}`, scope)

	// The result must keep every character bytewise, just wrapped in
	// the POSIX single-quote idiom that closes/reopens around each
	// embedded `'`.
	want := `echo 'before` + "`whoami`" + `mid$(id);x&y|z` + "\n" + `'\''after'`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSubstituteShellEmptyValue(t *testing.T) {
	// An empty value renders as an explicit empty argument (`''`),
	// not as nothing — preserves the argv shape so a downstream
	// command sees the same number of tokens regardless of whether
	// the var was set or empty.
	scope := chain.Scope{Vars: map[string]string{"x": ""}}
	got, _ := chain.SubstituteShell(`cmd ${x} after`, scope)
	want := `cmd '' after`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteShellNumericValue(t *testing.T) {
	// Numbers from JSON captures (decoded as float64) round-trip
	// correctly and end up shell-quoted as the string form.
	scope := chain.Scope{
		Captures: map[string]any{
			"pr": map[string]any{"number": float64(42)},
		},
	}
	got, _ := chain.SubstituteShell(`gaia pr merge ${pr.number}`, scope)
	want := `gaia pr merge '42'`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteShellSpaceOnlyValue(t *testing.T) {
	// A value of just spaces stays a single token (the surrounding
	// quotes preserve the whitespace) rather than disappearing into
	// a word break.
	scope := chain.Scope{Vars: map[string]string{"x": "   "}}
	got, _ := chain.SubstituteShell(`cmd ${x} done`, scope)
	want := `cmd '   ' done`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteShellLeavesUnresolvedLiteral(t *testing.T) {
	// Unresolved refs aren't quoted — they pass through as `${name}`
	// for the operator to spot. This matches Substitute's existing
	// dry-run-friendly behavior.
	got, unresolved := chain.SubstituteShell(`hello ${missing}`, chain.Scope{})
	if got != `hello ${missing}` {
		t.Errorf("got %q", got)
	}
	if !reflect.DeepEqual(unresolved, []string{"missing"}) {
		t.Errorf("unresolved: %+v", unresolved)
	}
}

func TestSubstituteRawIsUnchanged(t *testing.T) {
	// SubstituteRaw is the verbatim path used by on_failure.return
	// substitutions where the result is JSON-encoded, not handed to
	// a shell. A hostile-looking var must round-trip bytewise.
	scope := chain.Scope{Vars: map[string]string{"v": `'; ouch #`}}
	got, _ := chain.SubstituteRaw(`message: ${v}`, scope)
	want := `message: '; ouch #`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- Phase C / #149 ---

func TestSubstituteItemAndIndex(t *testing.T) {
	scope := chain.Scope{
		Captures: map[string]any{
			"item":  "alpha",
			"index": 3,
		},
	}
	got, _ := chain.Substitute(`got ${item} at ${index}`, scope)
	if got != "got alpha at 3" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteItemDottedField(t *testing.T) {
	scope := chain.Scope{
		Captures: map[string]any{
			"item": map[string]any{"number": float64(42), "title": "x"},
		},
	}
	got, _ := chain.Substitute(`issue ${item.number}: ${item.title}`, scope)
	if got != "issue 42: x" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteSliceIndexLookup(t *testing.T) {
	// for_each captures land under a slice; substitution into
	// ${name.<index>.<field>} must descend through the slice.
	scope := chain.Scope{
		Captures: map[string]any{
			"results": []any{
				map[string]any{"id": "first"},
				map[string]any{"id": "second"},
			},
		},
	}
	got, _ := chain.Substitute(`first=${results.0.id}, second=${results.1.id}`, scope)
	if got != "first=first, second=second" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteSliceIndexOutOfRange(t *testing.T) {
	scope := chain.Scope{
		Captures: map[string]any{
			"results": []any{map[string]any{"id": "only"}},
		},
	}
	got, unresolved := chain.Substitute(`miss=${results.5.id}`, scope)
	if got != `miss=${results.5.id}` {
		t.Errorf("expected literal placeholder; got %q", got)
	}
	if len(unresolved) != 1 || unresolved[0] != "results.5.id" {
		t.Errorf("unresolved: %v", unresolved)
	}
}

func TestSubstituteParallelSubStepCapture(t *testing.T) {
	// Parallel sub-step capture map shape: outer step's capture is
	// {sub-id: {field: value}}.
	scope := chain.Scope{
		Captures: map[string]any{
			"fan-out": map[string]any{
				"open-pr-a": map[string]any{"number": float64(101)},
				"open-pr-b": map[string]any{"number": float64(102)},
			},
		},
	}
	got, _ := chain.Substitute(`a=${fan-out.open-pr-a.number} b=${fan-out.open-pr-b.number}`, scope)
	// fan-out has a hyphen — not a valid identifier. The current
	// shape uses underscores for identifiers, so this would fall
	// out as unresolved. Capture names that include hyphens are
	// rejected at parse time anyway. Verify the underscore form
	// instead — same shape, just substituting the legal identifier.
	if got == "a=101 b=102" {
		// If the parser ever accepts hyphens this will pass — fine
		// either way.
		return
	}
	scope.Captures = map[string]any{
		"fanout": map[string]any{
			"open_pr_a": map[string]any{"number": float64(101)},
			"open_pr_b": map[string]any{"number": float64(102)},
		},
	}
	got, _ = chain.Substitute(`a=${fanout.open_pr_a.number} b=${fanout.open_pr_b.number}`, scope)
	if got != "a=101 b=102" {
		t.Errorf("got %q", got)
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
