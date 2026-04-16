package tui

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/filter"
)

// parseUDAFlags parses --uda flag values into a map.
// Each value must be in key=value format. Split on the first '=' only.
// Empty value (key=) is valid — it signals key deletion during merge.
// Returns nil if input is empty.
func parseUDAFlags(values []string) (map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]any, len(values))
	for _, v := range values {
		key, value, ok := strings.Cut(v, "=")
		if !ok {
			return nil, fmt.Errorf("invalid UDA format %q, expected key=value", v)
		}
		if key == "" {
			return nil, fmt.Errorf("empty UDA key in %q", v)
		}
		if err := domain.ValidateUDAKey(key); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

// reservedTaskFields is the allowlist of top-level field keys that
// tusk task create and tusk task modify accept in their inline syntax.
// Any other bare key is rejected as unknown by validateKnownFields;
// uda.* fields are handled separately by collectUDAs.
var reservedTaskFields = map[string]struct{}{
	"title":       {},
	"description": {},
	"project":     {},
	"priority":    {},
	"status":      {},
	"due":         {},
	"parent":      {},
}

// collectUDAs walks fs.Fields and returns a map of UDA key -> value
// for every field whose key begins with "uda.". The returned map is
// nil (not an empty map) when no uda.* field is present, so callers
// can distinguish "caller touched UDAs" from "caller did not".
//
// Duplicates resolve last-wins, matching the StringArray semantics
// of the old --uda flag.
//
// Errors:
//   - a uda.* field carrying a non-zero Modifier prefix is rejected
//     ("modifier %q not supported on uda fields", string(prefix))
//   - the tail after "uda." is validated via domain.ValidateUDAKey,
//     which rejects empty, digit-led, and dot-containing tails
func collectUDAs(fs *filter.FilterSet) (map[string]any, error) {
	var out map[string]any
	for _, f := range fs.Fields {
		key, ok := strings.CutPrefix(f.Key, "uda.")
		if !ok {
			continue
		}
		if f.Modifier != 0 {
			return nil, fmt.Errorf("modifier %q not supported on uda fields", string(f.Modifier))
		}
		if err := domain.ValidateUDAKey(key); err != nil {
			return nil, err
		}
		if out == nil {
			out = make(map[string]any)
		}
		out[key] = f.Value
	}
	return out, nil
}

// validateKnownFields returns an error if any field in fs.Fields is
// not in reservedTaskFields and does not have a "uda." prefix.
//
// Bare unknown keys (no dot) return a "did you mean uda.X?" hint
// to catch the common typo where a user forgets the uda prefix.
// Dotted unknown keys return a plain "unknown field" error — a dot
// in the key signals intent, so a did-you-mean hint would be noise.
func validateKnownFields(fs *filter.FilterSet) error {
	for _, f := range fs.Fields {
		if _, ok := reservedTaskFields[f.Key]; ok {
			continue
		}
		if strings.HasPrefix(f.Key, "uda.") {
			continue
		}
		if strings.Contains(f.Key, ".") {
			return fmt.Errorf("unknown field %q", f.Key)
		}
		return fmt.Errorf("unknown field %q; did you mean uda.%s?", f.Key, f.Key)
	}
	return nil
}
