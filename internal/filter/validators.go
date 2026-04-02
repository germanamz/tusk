package filter

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// fieldValidators maps field names to their validation functions.
// The parser uses this table to validate field values.
var fieldValidators = map[string]func(string) error{
	"status":   validateStatus,
	"project":  validateProject,
	"priority": validatePriority,
	"due":      validateDue,
	"parent":   validateShortID,
	"tree":     validateShortID,
	"waiting":  validateBool,
}

func validateStatus(v string) error {
	if v == "" {
		return fmt.Errorf("status value cannot be empty")
	}
	parts := strings.Split(v, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("status contains empty value in %q", v)
		}
	}
	return nil
}

func validateProject(v string) error {
	if v == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	return nil
}

func validatePriority(v string) error {
	if v == "" {
		return fmt.Errorf("priority value cannot be empty")
	}

	if strings.Contains(v, "..") {
		parts := strings.SplitN(v, "..", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid priority range %q: use min..max", v)
		}
		min, errMin := parsePriorityValue(parts[0])
		max, errMax := parsePriorityValue(parts[1])
		if errMin != nil {
			return fmt.Errorf("invalid priority range min %q: %w", parts[0], errMin)
		}
		if errMax != nil {
			return fmt.Errorf("invalid priority range max %q: %w", parts[1], errMax)
		}
		if min > max {
			return fmt.Errorf("invalid priority range: min (%d) must be <= max (%d)", min, max)
		}
		return nil
	}

	_, err := parsePriorityValue(v)
	return err
}

// parsePriorityValue converts a single priority string to an int.
// Accepts numeric (0-4) or named (none, low, medium, high, urgent).
func parsePriorityValue(s string) (int, error) {
	named := map[string]int{
		"none": 0, "low": 1, "medium": 2, "high": 3, "urgent": 4,
	}
	if v, ok := named[strings.ToLower(s)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 || v > 4 {
		return 0, fmt.Errorf("invalid priority %q: expected 0-4 or none/low/medium/high/urgent", s)
	}
	return v, nil
}

func validateDue(v string) error {
	if v == "" {
		return fmt.Errorf("due value cannot be empty")
	}

	// Range: "today..friday" or "2026-04-01..2026-04-10"
	if strings.Contains(v, "..") {
		parts := strings.SplitN(v, "..", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid due range %q: use start..end", v)
		}
		if err := validateSingleDue(parts[0]); err != nil {
			return fmt.Errorf("invalid due range start: %w", err)
		}
		if err := validateSingleDue(parts[1]); err != nil {
			return fmt.Errorf("invalid due range end: %w", err)
		}
		return nil
	}

	return validateSingleDue(v)
}

// validateSingleDue checks that a single due value is a recognized format.
func validateSingleDue(v string) error {
	lower := strings.ToLower(v)

	// Relative keywords
	switch lower {
	case "today", "tomorrow", "thisweek":
		return nil
	}

	// Weekday names
	weekdays := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	for _, w := range weekdays {
		if lower == w {
			return nil
		}
	}

	// RFC 3339
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return nil
	}

	// Date-only
	if _, err := time.Parse("2006-01-02", v); err == nil {
		return nil
	}

	return fmt.Errorf("invalid due date %q: use YYYY-MM-DD, RFC3339, today, tomorrow, thisweek, or a weekday name", v)
}

func validateShortID(v string) error {
	if len(v) < 4 {
		return fmt.Errorf("short ID %q is too short: minimum 4 hex characters", v)
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("short ID %q contains non-hex character %q", v, string(c))
		}
	}
	return nil
}

func validateBool(v string) error {
	if v != "true" && v != "false" {
		return fmt.Errorf("expected \"true\" or \"false\", got %q", v)
	}
	return nil
}
