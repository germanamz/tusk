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
func ParseRelativeDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("duration must not be empty")
	}

	var i int
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("duration %q must begin with a positive integer", s)
	}
	if i == len(s) {
		return 0, fmt.Errorf("duration %q missing unit suffix (d, w, h, m, s)", s)
	}

	numStr := s[:i]
	unit := s[i:]

	n, err := strconv.ParseUint(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing duration %q: %w", s, err)
	}
	if n == 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}

	switch unit {
	case "d":
		return time.Duration(n) * 24 * time.Hour, nil
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	case "h", "m", "s", "ms", "us", "ns":
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("parsing duration %q: %w", s, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("duration %q must be positive", s)
		}
		return d, nil
	default:
		return 0, fmt.Errorf("unknown duration unit %q in %q (expected d, w, h, m, s)", unit, s)
	}
}
