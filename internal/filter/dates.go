package filter

import (
	"fmt"
	"strings"
	"time"
)

// parseDate converts a string to a time.Time.
// Accepts: RFC 3339 ("2026-04-10T15:30:00Z"), date-only ("2026-04-10"),
// relative ("today", "tomorrow", "thisweek"), or weekday names ("monday"-"sunday").
func parseDate(s string) (time.Time, error) {
	lower := strings.ToLower(s)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	switch lower {
	case "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	case "thisweek":
		daysUntilSunday := int(time.Saturday - today.Weekday() + 1)
		if daysUntilSunday <= 0 {
			daysUntilSunday += 7
		}
		return today.AddDate(0, 0, daysUntilSunday), nil
	}

	// Weekday names
	weekdays := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday,
		"friday": time.Friday, "saturday": time.Saturday,
	}
	if target, ok := weekdays[lower]; ok {
		days := int(target - today.Weekday())
		if days <= 0 {
			days += 7
		}
		return today.AddDate(0, 0, days), nil
	}

	// RFC 3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Date-only
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD, RFC3339, today, tomorrow, thisweek, or a weekday name", s)
}

// parseDateRange splits a "start..end" string and parses both sides.
func parseDateRange(s string) (start, end time.Time, err error) {
	parts := strings.SplitN(s, "..", 2)
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date range %q: use start..end", s)
	}
	start, err = parseDate(parts[0])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid range start: %w", err)
	}
	end, err = parseDate(parts[1])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid range end: %w", err)
	}
	return start, end, nil
}
