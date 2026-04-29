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
func (result UrgencyParseResult) Empty() bool {
	return !result.ClearAll && len(result.Clear) == 0 && len(result.Set) == 0 && len(result.Delta) == 0
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
	for index, field := range fields {
		if field.Key == "urgency.clear" {
			if field.Modifier != 0 {
				return UrgencyParseResult{}, nil, fmt.Errorf("modifier %q not supported on %q", string(field.Modifier), field.Key)
			}
			switch field.Value {
			case "true":
				result.ClearAll = true
			case "false":
				// no-op, but consume the field
			default:
				return UrgencyParseResult{}, nil, fmt.Errorf("urgency.clear expects true or false, got %q", field.Value)
			}
			continue
		}
		if !strings.HasPrefix(field.Key, "urgency.") {
			notConsumed = append(notConsumed, index)
			continue
		}
		cfgKey, ok := urgencyCLIToConfigKey(field.Key)
		if !ok {
			notConsumed = append(notConsumed, index)
			continue
		}
		switch field.Modifier {
		case 0:
			if field.Value == "" {
				result.Clear[cfgKey] = true
				continue
			}
			floatVal, parseErr := strconv.ParseFloat(field.Value, 64)
			if parseErr != nil {
				return UrgencyParseResult{}, nil, fmt.Errorf("field %q: invalid float %q: %w", field.Key, field.Value, parseErr)
			}
			result.Set[cfgKey] = floatVal
		case '+':
			floatVal, parseErr := strconv.ParseFloat(field.Value, 64)
			if parseErr != nil {
				return UrgencyParseResult{}, nil, fmt.Errorf("field %q: invalid float %q: %w", field.Key, field.Value, parseErr)
			}
			result.Delta[cfgKey] = floatVal
		case '-':
			floatVal, parseErr := strconv.ParseFloat(field.Value, 64)
			if parseErr != nil {
				return UrgencyParseResult{}, nil, fmt.Errorf("field %q: invalid float %q: %w", field.Key, field.Value, parseErr)
			}
			result.Delta[cfgKey] = -floatVal
		default:
			return UrgencyParseResult{}, nil, fmt.Errorf("modifier %q not supported on %q", string(field.Modifier), field.Key)
		}
	}
	return result, notConsumed, nil
}
