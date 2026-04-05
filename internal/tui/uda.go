package tui

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
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
