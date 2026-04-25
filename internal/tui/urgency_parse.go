package tui

import (
	"fmt"
	"strconv"
	"strings"
)

// UrgencyParseResult is the structured output of parseUrgencyFields.
type UrgencyParseResult struct {
	ClearAll bool
	Clear    map[string]bool
	Set      map[string]float64
	Delta    map[string]float64
}

// Empty returns true when no urgency-related field was consumed.
func (r UrgencyParseResult) Empty() bool {
	return !r.ClearAll && len(r.Clear) == 0 && len(r.Set) == 0 && len(r.Delta) == 0
}

// urgencyFieldInput is the minimal shape parseUrgencyFields needs. Both
// filter.FieldFilter and syntax.FieldFilter fit through a small adapter.
type urgencyFieldInput struct {
	Key      string
	Value    string
	Modifier byte // 0, '+', or '-'
}

// parseUrgencyFields consumes urgency-flavored fields from an iterator and
// returns the structured result plus a list of indices that were NOT
// consumed (so callers can continue processing non-urgency fields).
func parseUrgencyFields(fields []urgencyFieldInput) (UrgencyParseResult, []int, error) {
	result := UrgencyParseResult{
		Clear: map[string]bool{},
		Set:   map[string]float64{},
		Delta: map[string]float64{},
	}
	var notConsumed []int
	for i, f := range fields {
		if f.Key == "urgency.clear" {
			if f.Modifier != 0 {
				return UrgencyParseResult{}, nil, fmt.Errorf("modifier %q not supported on %q", string(f.Modifier), f.Key)
			}
			switch f.Value {
			case "true":
				result.ClearAll = true
			case "false":
				// no-op, but consume the field
			default:
				return UrgencyParseResult{}, nil, fmt.Errorf("urgency.clear expects true or false, got %q", f.Value)
			}
			continue
		}
		if !strings.HasPrefix(f.Key, "urgency.") {
			notConsumed = append(notConsumed, i)
			continue
		}
		cfgKey, ok := urgencyCLIToConfigKey(f.Key)
		if !ok {
			notConsumed = append(notConsumed, i)
			continue
		}
		switch f.Modifier {
		case 0:
			if f.Value == "" {
				result.Clear[cfgKey] = true
				continue
			}
			v, err := strconv.ParseFloat(f.Value, 64)
			if err != nil {
				return UrgencyParseResult{}, nil, fmt.Errorf("field %q: invalid float %q: %w", f.Key, f.Value, err)
			}
			result.Set[cfgKey] = v
		case '+':
			v, err := strconv.ParseFloat(f.Value, 64)
			if err != nil {
				return UrgencyParseResult{}, nil, fmt.Errorf("field %q: invalid float %q: %w", f.Key, f.Value, err)
			}
			result.Delta[cfgKey] = v
		case '-':
			v, err := strconv.ParseFloat(f.Value, 64)
			if err != nil {
				return UrgencyParseResult{}, nil, fmt.Errorf("field %q: invalid float %q: %w", f.Key, f.Value, err)
			}
			result.Delta[cfgKey] = -v
		default:
			return UrgencyParseResult{}, nil, fmt.Errorf("modifier %q not supported on %q", string(f.Modifier), f.Key)
		}
	}
	return result, notConsumed, nil
}
