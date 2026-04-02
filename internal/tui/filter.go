package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParsedArgs holds the result of parsing CLI arguments.
type ParsedArgs struct {
	Title    string            // non-key:value, non-tag args joined with spaces
	Fields   map[string]string // key:value pairs
	Tags     []string          // +tag inclusions
	ExclTags []string          // -tag exclusions
}

// parseArgs classifies each arg in the slice into title words, key:value fields,
// +tags, or -tags.
//
// Rules:
//   - "key:value" -> Fields["key"] = "value" (first colon splits; value may contain colons)
//   - "+word"     -> Tags = append(Tags, "word")
//   - "-word"     -> ExclTags = append(ExclTags, "word")
//   - everything else is joined with spaces as Title
func parseArgs(args []string) ParsedArgs {
	p := ParsedArgs{Fields: make(map[string]string)}
	var titleParts []string

	for _, arg := range args {
		switch {
		case strings.Contains(arg, ":") && !strings.HasPrefix(arg, "+") && !strings.HasPrefix(arg, "-"):
			key, value, _ := strings.Cut(arg, ":")
			p.Fields[key] = value
		case strings.HasPrefix(arg, "+") && len(arg) > 1:
			p.Tags = append(p.Tags, arg[1:])
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			p.ExclTags = append(p.ExclTags, arg[1:])
		default:
			titleParts = append(titleParts, arg)
		}
	}

	p.Title = strings.Join(titleParts, " ")
	return p
}

// parseDate converts a string to a time.Time.
// Accepts: RFC 3339 ("2026-04-10T15:30:00Z"), date-only ("2026-04-10"),
// relative ("today", "tomorrow"), or weekday names ("monday"-"sunday").
func parseDate(s string) (time.Time, error) {
	lower := strings.ToLower(s)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	switch lower {
	case "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	}

	// Try weekday names
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

	// Try RFC 3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try date-only
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD, RFC3339, today, tomorrow, or a weekday name", s)
}

// parsePriority converts a string to a priority int (0-4).
// Accepts numeric ("0"-"4") or named ("none", "low", "medium", "high", "urgent").
func parsePriority(s string) (int, error) {
	named := map[string]int{
		"none": 0, "low": 1, "medium": 2, "high": 3, "urgent": 4,
	}
	if v, ok := named[strings.ToLower(s)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 || v > 4 {
		return 0, fmt.Errorf("invalid priority %q: must be 0-4 or none/low/medium/high/urgent", s)
	}
	return v, nil
}
