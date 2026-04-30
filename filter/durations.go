package filter

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseRelativeDuration parses a positive duration with an extended unit set.
// Accepts Go's standard ParseDuration units (ns, us, ms, s, m, h) plus:
//   - "d" — 24 hours
//   - "w" — 7 days
//
// The input must be a single unsigned numeric literal followed by one unit
// suffix; compound forms such as "1w2d" are rejected to keep --since inputs
// unambiguous. Zero and negative durations are rejected.
//
// Examples:
//
//	ParseRelativeDuration("7d")  → 7 * 24h
//	ParseRelativeDuration("2w")  → 14 * 24h
//	ParseRelativeDuration("24h") → 24h
//	ParseRelativeDuration("30m") → 30m
func ParseRelativeDuration(input string) (time.Duration, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, fmt.Errorf("duration must not be empty")
	}

	var digitEnd int
	for digitEnd < len(input) && input[digitEnd] >= '0' && input[digitEnd] <= '9' {
		digitEnd++
	}
	if digitEnd == 0 {
		return 0, fmt.Errorf("duration %q must begin with a positive integer", input)
	}
	if digitEnd == len(input) {
		return 0, fmt.Errorf("duration %q missing unit suffix (d, w, h, m, s)", input)
	}

	numStr := input[:digitEnd]
	unit := input[digitEnd:]

	count, parseErr := strconv.ParseUint(numStr, 10, 64)

	if parseErr != nil {
		return 0, fmt.Errorf("parsing duration %q: %w", input, parseErr)
	}
	if count == 0 {
		return 0, fmt.Errorf("duration %q must be positive", input)
	}

	switch unit {
	case "d":
		return time.Duration(count) * 24 * time.Hour, nil
	case "w":
		return time.Duration(count) * 7 * 24 * time.Hour, nil
	case "h", "m", "s", "ms", "us", "ns":
		dur, durErr := time.ParseDuration(input)

		if durErr != nil {
			return 0, fmt.Errorf("parsing duration %q: %w", input, durErr)
		}
		if dur <= 0 {
			return 0, fmt.Errorf("duration %q must be positive", input)
		}
		return dur, nil
	default:
		return 0, fmt.Errorf("unknown duration unit %q in %q (expected d, w, h, m, s)", unit, input)
	}
}
