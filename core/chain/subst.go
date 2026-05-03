package chain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Scope holds everything a step's `run:` line can reference. Vars
// are the chain's inputs (flat strings); Captures are previous
// steps' parsed envelope `data` payloads (typed any — usually a map
// or scalar). Lookup is via `${name}` for vars and `${name.path}`
// for captures.
//
// Naming collisions: if a var and a capture share a name, the var
// wins for `${name}` lookups and the capture wins for `${name.path}`
// lookups. In practice operators won't name an input the same as a
// capture; we just need behavior to be defined.
type Scope struct {
	Vars     map[string]string
	Captures map[string]any
}

// Substitute resolves `${name}` and `${name.path.to.field}` references
// in s. Returns the resolved string and a list of references that
// couldn't be resolved — callers decide whether to error or render
// them as <unset:name> for dry-run visibility.
//
// Resolution order for `${name}` (no dot):
//
//  1. Vars[name] — chain input
//  2. Captures[name] — fall back to the whole captured object
//     (rendered as JSON when not a string)
//  3. unresolved
//
// For `${name.path}` (with dots), first segment is always a capture
// key; subsequent segments descend into the JSON object via map
// keys (or array indices for numeric segments — though Phase A
// doesn't ship a use case for that).
func Substitute(s string, scope Scope) (resolved string, unresolved []string) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		// Look for ${
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end == -1 {
				// Unterminated ${ — write rest literally and bail.
				b.WriteString(s[i:])
				break
			}
			ref := s[i+2 : i+2+end]
			val, ok := lookup(ref, scope)
			if !ok {
				unresolved = append(unresolved, ref)
				b.WriteString("${")
				b.WriteString(ref)
				b.WriteByte('}')
			} else {
				b.WriteString(val)
			}
			i += 2 + end + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), unresolved
}

// lookup resolves a single ref string (the contents of `${...}`)
// against the scope. Returns (value, true) on success, ("", false)
// on miss.
func lookup(ref string, scope Scope) (string, bool) {
	// Validate the ref looks like a name or name.path. Anything else
	// (spaces, weird chars) is unresolvable — callers see it in the
	// unresolved list.
	if ref == "" {
		return "", false
	}
	parts := strings.Split(ref, ".")
	for _, p := range parts {
		if p == "" || !isValidIdent(p) {
			return "", false
		}
	}

	// Single segment: try vars, then captures.
	if len(parts) == 1 {
		if v, ok := scope.Vars[parts[0]]; ok {
			return v, true
		}
		if v, ok := scope.Captures[parts[0]]; ok {
			return stringify(v), true
		}
		return "", false
	}

	// Dotted: capture lookup. First segment names the capture, rest
	// descends.
	root, ok := scope.Captures[parts[0]]
	if !ok {
		return "", false
	}
	cur := root
	for _, p := range parts[1:] {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[p]
		if !ok {
			return "", false
		}
	}
	return stringify(cur), true
}

// stringify renders a captured value for shell-substitution context.
// Strings pass through; numbers and bools use Go's default
// formatting; everything else (maps, arrays) is rendered as compact
// JSON so it's still useful when an agent wants to pass a whole
// payload to a child command.
func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// JSON numbers decode as float64. Keep integers integer-shaped
		// when they're representable as such (no .000 trailing).
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case int, int32, int64:
		return fmt.Sprintf("%d", x)
	default:
		raw, _ := json.Marshal(x)
		return string(raw)
	}
}
