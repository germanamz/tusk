package filter

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// priorityNames maps named priority levels to their numeric values.
var priorityNames = map[string]int{
	"none": 0, "low": 1, "medium": 2, "high": 3, "urgent": 4,
}

func validateStatus(value string) error {
	if value == "" {
		return fmt.Errorf("status value cannot be empty")
	}
	parts := strings.Split(value, ",")
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("status contains empty value in %q", value)
		}
	}
	return nil
}

func validateProject(value string) error {
	if value == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	return nil
}

func validatePriority(value string) error {
	if value == "" {
		return fmt.Errorf("priority value cannot be empty")
	}

	if strings.Contains(value, "..") {
		parts := strings.SplitN(value, "..", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid priority range %q: use min..max", value)
		}
		minVal, minErr := parsePriorityValue(parts[0])
		maxVal, maxErr := parsePriorityValue(parts[1])

		if minErr != nil {
			return fmt.Errorf("invalid priority range min %q: %w", parts[0], minErr)
		}

		if maxErr != nil {
			return fmt.Errorf("invalid priority range max %q: %w", parts[1], maxErr)
		}
		if minVal > maxVal {
			return fmt.Errorf("invalid priority range: min (%d) must be <= max (%d)", minVal, maxVal)
		}
		return nil
	}

	_, err := parsePriorityValue(value)
	return err
}

// parsePriorityValue converts a single priority string to an int.
// Accepts numeric (0-4) or named (none, low, medium, high, urgent).
func parsePriorityValue(input string) (int, error) {
	if named, ok := priorityNames[strings.ToLower(input)]; ok {
		return named, nil
	}
	num, err := strconv.Atoi(input)
	if err != nil || num < 0 || num > 4 {
		return 0, fmt.Errorf("invalid priority %q: expected 0-4 or none/low/medium/high/urgent", input)
	}
	return num, nil
}

func validateDue(value string) error {
	if value == "" {
		return nil // empty means "clear" in modify context
	}

	// Range: "today..friday" or "2026-04-01..2026-04-10"
	if strings.Contains(value, "..") {
		parts := strings.SplitN(value, "..", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid due range %q: use start..end", value)
		}
		if err := validateSingleDue(parts[0]); err != nil {
			return fmt.Errorf("invalid due range start: %w", err)
		}
		if err := validateSingleDue(parts[1]); err != nil {
			return fmt.Errorf("invalid due range end: %w", err)
		}
		return nil
	}

	return validateSingleDue(value)
}

// validateSingleDue checks that a single due value is a recognized format.
func validateSingleDue(value string) error {
	lower := strings.ToLower(value)

	switch lower {
	case "today", "tomorrow", "thisweek",
		"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday":
		return nil
	}

	// RFC 3339
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return nil
	}

	// Date-only
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return nil
	}

	return fmt.Errorf("invalid due date %q: use YYYY-MM-DD, RFC3339, today, tomorrow, thisweek, or a weekday name", value)
}

func validateShortID(value string) error {
	if value == "" {
		return nil // empty means "clear" in modify context
	}
	if len(value) < 4 {
		return fmt.Errorf("short ID %q is too short: minimum 4 hex characters", value)
	}
	for idx := 0; idx < len(value); idx++ {
		char := value[idx]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return fmt.Errorf("short ID %q contains non-hex character %q", value, string(char))
		}
	}
	return nil
}

func validateBool(value string) error {
	if value != "true" && value != "false" {
		return fmt.Errorf("expected \"true\" or \"false\", got %q", value)
	}
	return nil
}

func validateNonEmpty(value string) error {
	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}
	return nil
}

// validateAny accepts any value, including empty. Used by fields where an
// empty value has a defined meaning (e.g. `description=` clears the
// description on `tusk task modify`).
func validateAny(_ string) error { return nil }

// ParsePriorityValue is the exported version of parsePriorityValue for use
// by the TUI layer when creating tasks (not filtering).
func ParsePriorityValue(input string) (int, error) {
	return parsePriorityValue(input)
}

// validateOrder accepts:
//   - empty string (matches IS NULL in resolver, clears in modify context)
//   - a single float (exact match)
//   - a range "a..b" where both sides parse as floats and a <= b
//
// Rejects comma-separated lists and any modifier prefix (handled at token
// level — filter/resolve.go never sees a Modifier on numeric fields).
func validateOrder(value string) error {
	if value == "" {
		return nil
	}
	if strings.Contains(value, ",") {
		return fmt.Errorf("order does not accept comma-separated values; use a single value or a..b range")
	}
	if strings.Contains(value, "..") {
		parts := strings.SplitN(value, "..", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid order range %q: use min..max", value)
		}
		lo, minErr := strconv.ParseFloat(parts[0], 64)

		if minErr != nil {
			return fmt.Errorf("invalid order range min %q: %w", parts[0], minErr)
		}

		hi, maxErr := strconv.ParseFloat(parts[1], 64)

		if maxErr != nil {
			return fmt.Errorf("invalid order range max %q: %w", parts[1], maxErr)
		}
		if lo > hi {
			return fmt.Errorf("invalid order range: min (%g) must be <= max (%g)", lo, hi)
		}
		return nil
	}
	if _, err := strconv.ParseFloat(value, 64); err != nil {
		return fmt.Errorf("invalid order value %q: %w", value, err)
	}
	return nil
}
