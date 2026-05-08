package envelope_test

import (
	"testing"

	"github.com/stewartbrothers/gaia/core/envelope"
	"github.com/stewartbrothers/gaia/core/types"
)

func TestScalarPlainString(t *testing.T) {
	e := envelope.New(map[string]any{"tag_name": "v1.2.3", "draft": false})
	got, err := e.Scalar("tag_name")
	if err != nil {
		t.Fatalf("Scalar: %v", err)
	}
	if got != "v1.2.3" {
		t.Errorf("got %q, want %q", got, "v1.2.3")
	}
}

func TestScalarBool(t *testing.T) {
	e := envelope.New(map[string]any{"draft": true})
	got, err := e.Scalar("draft")
	if err != nil {
		t.Fatalf("Scalar: %v", err)
	}
	if got != "true" {
		t.Errorf("got %q, want %q", got, "true")
	}
}

func TestScalarNumber(t *testing.T) {
	e := envelope.New(map[string]any{"number": float64(42)})
	got, err := e.Scalar("number")
	if err != nil {
		t.Fatalf("Scalar: %v", err)
	}
	if got != "42" {
		t.Errorf("got %q, want %q", got, "42")
	}
}

func TestScalarNestedPath(t *testing.T) {
	e := envelope.New(map[string]any{
		"head": map[string]any{"sha": "abc123def"},
	})
	got, err := e.Scalar("head.sha")
	if err != nil {
		t.Fatalf("Scalar: %v", err)
	}
	if got != "abc123def" {
		t.Errorf("got %q, want %q", got, "abc123def")
	}
}

func TestScalarTrustTaggedField(t *testing.T) {
	// trust-tagged fields (issue body, PR title, etc.) are emitted as
	// {"_trust":"external","_value":"..."} — Scalar must unwrap and
	// return the inner value rather than the object shape.
	issue := &types.Issue{Number: 1, Title: "hello world", Body: "some body"}
	e := envelope.New(issue)
	got, err := e.Scalar("title")
	if err != nil {
		t.Fatalf("Scalar: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestScalarMissingPathErrors(t *testing.T) {
	e := envelope.New(map[string]any{"a": "b"})
	_, err := e.Scalar("no_such_field")
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestScalarObjectErrors(t *testing.T) {
	e := envelope.New(map[string]any{"author": map[string]any{"login": "alice"}})
	_, err := e.Scalar("author")
	if err == nil {
		t.Fatal("expected error when extracting a non-scalar object")
	}
}

func TestScalarArrayErrors(t *testing.T) {
	e := envelope.New(map[string]any{"labels": []any{"bug", "enhancement"}})
	_, err := e.Scalar("labels")
	if err == nil {
		t.Fatal("expected error when extracting an array")
	}
}
