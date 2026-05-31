// Package argval coerces loosely-typed argument maps — the map[string]any an
// alias invocation or tool call carries — into the typed values request
// builders need. Each coercer treats an absent key as the zero value with no
// error and returns a typed error when a present value has the wrong type. The
// error text is a stable contract: it surfaces (wrapped) to alias/tool callers.
package argval

import "fmt"

// String returns args[key] as a string, or "" if absent. Returns an error if
// the value is present but the wrong type.
func String(args map[string]any, key string) (string, error) {
	raw, ok := args[key]

	if !ok {
		return "", nil
	}

	typed, isString := raw.(string)

	if !isString {
		return "", fmt.Errorf("arg %q has type %T, want string", key, raw)
	}

	return typed, nil
}

// Int returns args[key] as an int. BurntSushi/toml decodes integers as int64
// when the destination is map[string]any; Int also accepts float64 for
// exact-integer values so the helper is resilient to JSON-bridged callers.
func Int(args map[string]any, key string) (int, error) {
	raw, ok := args[key]

	if !ok {
		return 0, nil
	}

	switch typed := raw.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		if typed != float64(int64(typed)) {
			return 0, fmt.Errorf("arg %q has type %T (non-integer float), want int", key, raw)
		}

		return int(typed), nil
	}

	return 0, fmt.Errorf("arg %q has type %T, want int", key, raw)
}

// Float returns args[key] as a float64.
func Float(args map[string]any, key string) (float64, error) {
	raw, ok := args[key]

	if !ok {
		return 0, nil
	}

	switch typed := raw.(type) {
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	}

	return 0, fmt.Errorf("arg %q has type %T, want float64", key, raw)
}

// Bool returns args[key] as a bool.
func Bool(args map[string]any, key string) (bool, error) {
	raw, ok := args[key]

	if !ok {
		return false, nil
	}

	typed, isBool := raw.(bool)

	if !isBool {
		return false, fmt.Errorf("arg %q has type %T, want bool", key, raw)
	}

	return typed, nil
}

// StringSlice returns args[key] as a []string. A bare string is also accepted
// (single value), matching Cobra's StringSlice semantics.
func StringSlice(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]

	if !ok {
		return nil, nil
	}

	if str, isString := raw.(string); isString {
		return []string{str}, nil
	}

	typed, isSlice := raw.([]any)

	if !isSlice {
		return nil, fmt.Errorf("arg %q has type %T, want []string", key, raw)
	}

	out := make([]string, 0, len(typed))

	for index, item := range typed {
		str, isString := item.(string)

		if !isString {
			return nil, fmt.Errorf("arg %q element %d has type %T, want string", key, index, item)
		}

		out = append(out, str)
	}

	return out, nil
}
