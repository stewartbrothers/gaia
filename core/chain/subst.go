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
// in s without any shell-quoting. Equivalent to SubstituteRaw — kept
// as the package's named entry point so callers that don't hand the
// result to a shell (`on_failure.return` map values, dry-run renders,
// etc.) stay readable.
//
// For run lines that are about to be passed to `sh -c`, callers MUST
// use SubstituteShell instead so substituted values are quoted as
// shell-literal data and can't inject metacharacters. See #135 for the
// security background.
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
	return SubstituteRaw(s, scope)
}

// SubstituteRaw is the verbatim-substitution variant: each resolved
// value is spliced into the result with no transformation. Use this
// for contexts that aren't a shell command — e.g. on_failure.return
// map values that the chain runner serializes as JSON, or dry-run
// renderings shown to the operator.
func SubstituteRaw(s string, scope Scope) (resolved string, unresolved []string) {
	return substituteWith(s, scope, func(v string) string { return v })
}

// SubstituteShell is the shell-safe substitution variant: each
// resolved value is wrapped via shellQuote before being spliced into
// the result, so substituted vars/captures cannot inject shell
// metacharacters into the surrounding `run:` string.
//
// Mitigation for #135: a hostile var (`'; rm -rf $HOME #`) or a
// hostile forge response captured into a downstream step's `run:` is
// rendered as a single-quoted shell literal, not as new shell tokens.
//
// The chain runner uses SubstituteShell at every call site whose
// result is fed to `sh -c`. Static parts of the run line — the verbs
// and flags written by the chain author — are NOT quoted, so a chain
// like `run: gaia pr merge ${pr.number}` still works the way the
// author expects. Only the values from ${...} substitutions get the
// quoting treatment.
//
// Authors who genuinely need to interpret a substituted value as
// shell tokens can wrap their own indirection: `run: sh -c "${cmd}"`
// where ${cmd} is intended as a script body. That's an explicit,
// auditable opt-in; the default is safe.
func SubstituteShell(s string, scope Scope) (resolved string, unresolved []string) {
	return substituteWith(s, scope, shellQuote)
}

// substituteWith is the single source of truth for the parsing logic.
// transform is applied to each successfully resolved value before it's
// written into the output buffer; unresolved refs are written back
// literally as `${ref}` regardless of the transform.
func substituteWith(s string, scope Scope, transform func(string) string) (resolved string, unresolved []string) {
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
				b.WriteString(transform(val))
			}
			i += 2 + end + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), unresolved
}

// shellQuote wraps s in single quotes for safe interpolation into a
// `sh -c`-style command line. The only character that breaks a
// single-quoted POSIX shell context is the single quote itself; we
// close the quoted run, emit an escaped quote, and reopen — the
// canonical close-escape-reopen idiom (one literal embedded quote
// becomes a four-char run: close-quote, backslash, quote,
// open-quote). Empty strings render as a pair of empty single
// quotes — an explicit empty argument — rather than nothing, so an
// unset-but-resolvable value doesn't silently disappear from the
// command line.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// lookupRaw resolves a ref against the scope and returns the raw
// underlying value (no stringification). Used by Phase C for_each
// to peek at the captured array shape before iterating, and by
// chain composition to pull whole sub-objects through. Returns
// (value, true) on success, (nil, false) on miss.
//
// Resolution mirrors lookup(): single-segment refs try Vars then
// Captures; dotted refs descend Captures.
func lookupRaw(ref string, scope Scope) (any, bool) {
	if ref == "" {
		return nil, false
	}
	parts := strings.Split(ref, ".")
	for _, p := range parts {
		if p == "" || !isValidIdent(p) {
			return nil, false
		}
	}
	if len(parts) == 1 {
		if v, ok := scope.Vars[parts[0]]; ok {
			return v, true
		}
		if v, ok := scope.Captures[parts[0]]; ok {
			return v, true
		}
		return nil, false
	}
	root, ok := scope.Captures[parts[0]]
	if !ok {
		return nil, false
	}
	cur := root
	for _, p := range parts[1:] {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
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
