package filter

import (
	"fmt"
	"strings"
)

// SortKey is one ORDER BY column produced from a --sort spec.
type SortKey struct {
	Property   string
	Descending bool
}

// ParseSort parses a --sort spec like "+priority,-due,+modified".
func ParseSort(spec string) ([]SortKey, error) {
	trimmedInput := strings.TrimSpace(spec)

	if trimmedInput == "" {
		return nil, nil
	}

	rawKeys := strings.Split(trimmedInput, ",")
	keys := make([]SortKey, 0, len(rawKeys))

	for index, raw := range rawKeys {
		trimmed := strings.TrimSpace(raw)

		if trimmed == "" {
			return nil, fmt.Errorf("sort: empty key at position %d", index)
		}

		key := SortKey{}

		switch trimmed[0] {
		case '+':
			key.Descending = false
			trimmed = trimmed[1:]
		case '-':
			key.Descending = true
			trimmed = trimmed[1:]
		default:
			key.Descending = false
		}

		if trimmed == "" {
			return nil, fmt.Errorf("sort: bare sign at position %d (expected property name after + or -)", index)
		}

		key.Property = trimmed
		keys = append(keys, key)
	}

	return keys, nil
}
