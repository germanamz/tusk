package domain

import (
	"fmt"
	"strings"
)

// ValidUrgencyWeightKeys lists the 10 keys accepted in any urgency-overrides
// input. Exported so CLI/MCP error messages can render the same set.
var ValidUrgencyWeightKeys = []string{
	"priority_weight", "due_weight", "age_weight", "active_weight",
	"blocking_weight", "blocked_weight", "tags_weight", "project_weight",
	"annotations_weight", "waiting_weight",
}

// validUrgencyWeightKeySet is a lookup table built from ValidUrgencyWeightKeys
// for O(1) key-validity checks.
var validUrgencyWeightKeySet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(ValidUrgencyWeightKeys))
	for _, k := range ValidUrgencyWeightKeys {
		m[k] = struct{}{}
	}
	return m
}()

// ValidateUrgencyOverridesPatch accepts only the 10 known weight keys and
// values that are JSON numbers or explicit nil. Typo-friendly error names
// the offending key and lists valid keys.
//
// Accepted value types: nil (will map to a Clear later), float64, float32,
// int, int64. The MCP JSON decoder may deliver any of these depending on
// transport and codec settings, so all are coerced upstream.
func ValidateUrgencyOverridesPatch(raw map[string]any) error {
	for key, value := range raw {
		if _, ok := validUrgencyWeightKeySet[key]; !ok {
			return fmt.Errorf("unknown urgency weight %q; valid keys: %s", key, strings.Join(ValidUrgencyWeightKeys, ", "))
		}
		switch value.(type) {
		case nil, float64, float32, int, int64:
			// accepted
		default:
			return fmt.Errorf("urgency weight %q must be a number or null, got %T", key, value)
		}
	}
	return nil
}
