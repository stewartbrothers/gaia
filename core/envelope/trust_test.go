package envelope_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/envelope"
	"github.com/stewartbrothers/gaia/core/types"
)

// TestTrustWalkerPreservesDeclarationOrder pins the #148 fix: when the
// trust walker rewrites a struct that contains externally-tagged
// fields, the resulting JSON must emit fields in struct-declaration
// order rather than alphabetical (which is what `map[string]any`
// produced before #148 — see #146 for where the regression landed).
//
// Canonical-JSON signing, deterministic cache keys, and human-friendly
// goldens all depend on this order being stable. Future walker changes
// that re-introduce alphabetical ordering trip this test.
func TestTrustWalkerPreservesDeclarationOrder(t *testing.T) {
	// types.Issue declares fields in this order:
	//   Number, Title, State, Author, Labels, Assignees, Body,
	//   CreatedAt, UpdatedAt, ClosedAt, Comments.
	// Empty-with-omitempty fields (Assignees, ClosedAt, Comments) are
	// dropped, so the expected on-wire order is:
	//   number, title, state, author, labels, body, created_at, updated_at.
	issue := &types.Issue{
		Number: 42,
		Title:  "answer",
		State:  "open",
		Author: types.User{Login: "alice"},
		Labels: []types.Label{{Name: "p1"}},
		Body:   "what's the answer?",
	}
	e := envelope.New(issue)
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)

	// Find the data subtree's keys in the order they appear on the
	// wire. Indexing into the marshalled byte stream keeps us honest
	// about the actual emitted order — decoding into a map would
	// silently re-sort.
	want := []string{"number", "title", "state", "author", "labels", "body", "created_at", "updated_at"}
	got := orderOfKeys(wire, want)
	if !equalSlices(got, want) {
		t.Errorf("data field order on the wire:\n  got:  %v\n  want: %v\nwire: %s", got, want, wire)
	}
}

// TestTrustWalkerNestedDeclarationOrder pins declaration order on a
// nested struct. Issue.Comments[] each carry tagged Body fields; the
// per-comment field order must also follow declaration order.
func TestTrustWalkerNestedDeclarationOrder(t *testing.T) {
	// types.Comment declares:
	//   ID, Author, Body, CreatedAt, UpdatedAt
	issue := &types.Issue{
		Number: 1,
		Title:  "x",
		Body:   "outer",
		Comments: []types.Comment{
			{ID: 7, Author: types.User{Login: "alice"}, Body: "first"},
		},
	}
	e := envelope.New(issue)
	b, _ := json.Marshal(e)
	wire := string(b)

	// Snip out the first comment object — it's the only one — and
	// look at its key order. The comment object starts at the
	// "comments" key.
	commentsIdx := strings.Index(wire, `"comments":[`)
	if commentsIdx < 0 {
		t.Fatalf("comments key missing on wire: %s", wire)
	}
	commentSlice := wire[commentsIdx:]
	want := []string{"id", "author", "body", "created_at", "updated_at"}
	got := orderOfKeys(commentSlice, want)
	if !equalSlices(got, want) {
		t.Errorf("nested Comment field order on the wire:\n  got:  %v\n  want: %v\nwire: %s", got, want, wire)
	}
}

// orderOfKeys returns the subset of `wanted` keys in the order they
// appear in s, scanning for `"key":` substrings. Callers pass the
// expected key set so we don't false-match on JSON values that happen
// to contain key-like text (e.g., a Body string with a colon).
func orderOfKeys(s string, wanted []string) []string {
	type pos struct {
		key string
		at  int
	}
	var hits []pos
	for _, k := range wanted {
		idx := strings.Index(s, `"`+k+`":`)
		if idx >= 0 {
			hits = append(hits, pos{key: k, at: idx})
		}
	}
	// Stable sort by position.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j-1].at > hits[j].at; j-- {
			hits[j-1], hits[j] = hits[j], hits[j-1]
		}
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.key)
	}
	return out
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
