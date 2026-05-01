package envelope

import "strings"

// FieldSpec is a tree of field paths to keep when projecting a value.
// A leaf (empty FieldSpec) means "include this whole subtree"; a
// non-leaf means "recurse into this subtree, only keeping the named
// children".
type FieldSpec map[string]FieldSpec

// ParseFields parses a comma-separated, dot-nested field spec (e.g.
// "number,title,labels.name") into a FieldSpec tree. Whitespace around
// paths is trimmed; empty path components are ignored. Empty input
// yields an empty FieldSpec, which Apply treats as identity.
func ParseFields(spec string) FieldSpec {
	out := FieldSpec{}
	for _, path := range strings.Split(spec, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cur := out
		parts := strings.Split(path, ".")
		for _, p := range parts {
			next, ok := cur[p]
			if !ok {
				next = FieldSpec{}
				cur[p] = next
			}
			cur = next
		}
	}
	return out
}

// Apply walks v and returns a filtered copy that only contains the
// paths listed in fs. Maps drop unlisted keys, arrays apply fs to each
// element, scalars are returned as-is. An empty FieldSpec is the
// identity — used so callers don't have to special-case "no
// projection".
//
// Best-effort semantics: a path that descends past a scalar is silently
// truncated to the scalar (e.g. spec "a.b" on `{a: 5}` returns
// `{a: 5}`). Unlisted keys at any level are dropped.
func (fs FieldSpec) Apply(v any) any {
	if len(fs) == 0 {
		return v
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(fs))
		for k, sub := range fs {
			val, ok := x[k]
			if !ok {
				continue
			}
			out[k] = sub.Apply(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = fs.Apply(e)
		}
		return out
	default:
		// scalar (string/number/bool/null) or nil — return as-is even
		// if a field path tried to descend past it.
		return v
	}
}
