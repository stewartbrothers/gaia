package envelope

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Scalar extracts a single scalar value from the envelope's Data at
// the given dot-path (e.g. "tag_name", "head.sha", "author.login").
//
// Trust-tagged fields are unwrapped: a field emitted as
// {"_trust":"external","_value":"..."} returns the inner string rather
// than the object shape, so callers get the actual text regardless of
// whether the field carries the external-trust marker.
//
// Returns an error if the path is absent, if the value at the path is
// an object or array (non-scalar), or if the data cannot be marshaled.
func (e *Envelope) Scalar(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("envelope: Scalar requires a non-empty field path")
	}

	tagged := applyTrustTags(e.Data)
	raw, err := json.Marshal(tagged)
	if err != nil {
		return "", fmt.Errorf("envelope: marshal data for scalar: %w", err)
	}

	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return "", fmt.Errorf("envelope: unmarshal data for scalar: %w", err)
	}

	parts := strings.Split(path, ".")
	cur := tree
	for _, part := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("envelope: cannot descend into non-object at %q", part)
		}
		val, exists := m[part]
		if !exists {
			return "", fmt.Errorf("envelope: field %q not found", path)
		}
		cur = val
	}

	return scalarToString(cur, path)
}

// scalarToString converts a leaf JSON value to its string representation.
// Trust-tagged objects {"_trust":"external","_value":"..."} are unwrapped.
// Objects and arrays are rejected.
func scalarToString(v any, path string) (string, error) {
	switch x := v.(type) {
	case map[string]any:
		// Unwrap trust-tagged external content.
		if trust, ok := x["_trust"]; ok && trust == "external" {
			if val, ok := x["_value"].(string); ok {
				return val, nil
			}
		}
		return "", fmt.Errorf("envelope: field %q is an object, not a scalar; use --format json to see the full value", path)
	case []any:
		return "", fmt.Errorf("envelope: field %q is an array, not a scalar; use --format json to see the full value", path)
	case string:
		return x, nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64:
		// JSON numbers decode to float64. Format as integer when there is
		// no fractional part (the common case for IDs, counts, etc.).
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x)), nil
		}
		return fmt.Sprintf("%g", x), nil
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%v", x), nil
	}
}
