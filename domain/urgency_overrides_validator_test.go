package domain

import (
	"strings"
	"testing"
)

func TestValidateUrgencyOverridesPatch_Valid(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"empty map", map[string]any{}},
		{"nil map", nil},
		{"float64 value", map[string]any{"priority_weight": float64(1.5)}},
		{"float32 value", map[string]any{"due_weight": float32(2.5)}},
		{"int value", map[string]any{"age_weight": int(3)}},
		{"int64 value", map[string]any{"active_weight": int64(4)}},
		{"nil value (clear)", map[string]any{"blocking_weight": nil}},
		{"all keys", map[string]any{
			"priority_weight":    1.0,
			"due_weight":         2.0,
			"age_weight":         3.0,
			"active_weight":      4.0,
			"blocking_weight":    5.0,
			"blocked_weight":     6.0,
			"tags_weight":        7.0,
			"project_weight":     8.0,
			"annotations_weight": 9.0,
			"waiting_weight":     10.0,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateUrgencyOverridesPatch(tc.raw); err != nil {
				t.Errorf("ValidateUrgencyOverridesPatch(%v) = %v, want nil", tc.raw, err)
			}
		})
	}
}

func TestValidateUrgencyOverridesPatch_UnknownKey(t *testing.T) {
	err := ValidateUrgencyOverridesPatch(map[string]any{"unknown_weight": 1.0})
	if err == nil {
		t.Fatalf("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "unknown_weight") {
		t.Errorf("error message should name the offending key: %v", err)
	}
	for _, k := range ValidUrgencyWeightKeys {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error message should list valid key %q: %v", k, err)
		}
	}
}

func TestValidateUrgencyOverridesPatch_InvalidValueType(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{"string", "abc"},
		{"bool", true},
		{"slice", []float64{1, 2}},
		{"map", map[string]any{"x": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUrgencyOverridesPatch(map[string]any{"priority_weight": tc.value})
			if err == nil {
				t.Fatalf("expected error for value type %T, got nil", tc.value)
			}
			if !strings.Contains(err.Error(), "priority_weight") {
				t.Errorf("error message should name the offending key: %v", err)
			}
		})
	}
}
