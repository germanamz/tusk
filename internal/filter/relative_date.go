package filter

import (
	"strconv"
	"strings"
	"time"
)

// relativeDate is a date literal measured from the clock: `today` (local
// midnight) or `now`, with an optional signed offset such as today-90d,
// today+2w or now-48h. The units are h (hours), d (days), w (weeks), m
// (calendar months) and y (years); there is no minutes unit, so m is never
// ambiguous. Only an unquoted value is a relative date, the way only an
// unquoted value is a pattern.
type relativeDate struct {
	anchor string // "today" or "now"
	amount int    // signed offset; 0 with no unit means no offset
	unit   byte   // one of relativeDateUnits, or 0 with no offset
}

const relativeDateUnits = "hdwmy"

// maxRelativeDigits caps the offset at 999999 units, far past any date a
// vault holds, so the arithmetic never overflows.
const maxRelativeDigits = 6

// parseRelativeDate parses raw as a relative date literal. Validate and
// Compile both call it, so they never disagree about what is one.
func parseRelativeDate(raw string) (relativeDate, bool) {
	var anchor string

	switch {
	case strings.HasPrefix(raw, "today"):
		anchor = "today"
	case strings.HasPrefix(raw, "now"):
		anchor = "now"
	default:
		return relativeDate{}, false
	}

	offset := raw[len(anchor):]

	if offset == "" {
		return relativeDate{anchor: anchor}, true
	}

	if len(offset) < 3 || (offset[0] != '+' && offset[0] != '-') {
		return relativeDate{}, false
	}

	digits := offset[1 : len(offset)-1]
	unit := offset[len(offset)-1]

	if len(digits) > maxRelativeDigits || strings.IndexByte(relativeDateUnits, unit) < 0 || strings.Trim(digits, "0123456789") != "" {
		return relativeDate{}, false
	}

	amount, convErr := strconv.Atoi(digits)

	if convErr != nil {
		return relativeDate{}, false
	}

	if offset[0] == '-' {
		amount = -amount
	}

	return relativeDate{anchor: anchor, amount: amount, unit: unit}, true
}

// looksRelative reports whether raw opens like a relative date, so a typo in
// one (today-5x) is reported rather than compared as text.
func looksRelative(raw string) bool {
	return strings.HasPrefix(raw, "today") || strings.HasPrefix(raw, "now")
}

// resolve returns the instant the literal names on now's clock and in its
// location: local midnight for today, now itself for now, moved by the offset.
func (rel relativeDate) resolve(now time.Time) time.Time {
	base := now

	if rel.anchor == "today" {
		year, month, day := now.Date()
		base = time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	}

	switch rel.unit {
	case 'h':
		return base.Add(time.Duration(rel.amount) * time.Hour)
	case 'd':
		return base.AddDate(0, 0, rel.amount)
	case 'w':
		return base.AddDate(0, 0, 7*rel.amount)
	case 'm':
		return addMonthsClamped(base, rel.amount)
	case 'y':
		return addMonthsClamped(base, 12*rel.amount)
	}

	return base
}

// addMonthsClamped moves base by months, keeping its day of the month unless
// the target month is shorter, in which case it lands on that month's last
// day. time.AddDate would roll March 31 minus a month over to March 3.
func addMonthsClamped(base time.Time, months int) time.Time {
	year, month, day := base.Date()
	firstOfTarget := time.Date(year, month+time.Month(months), 1, 0, 0, 0, 0, base.Location())
	lastDay := time.Date(firstOfTarget.Year(), firstOfTarget.Month()+1, 0, 0, 0, 0, 0, base.Location()).Day()

	return time.Date(firstOfTarget.Year(), firstOfTarget.Month(), min(day, lastDay), base.Hour(), base.Minute(), base.Second(), base.Nanosecond(), base.Location())
}

// formatTemporal renders instant the way the indexer stores a value of
// propertyType, so the two compare as text: a date is the calendar date in
// instant's own location, and a datetime is RFC3339 in UTC.
func formatTemporal(instant time.Time, propertyType string) string {
	if propertyType == "date" {
		return instant.Format(time.DateOnly)
	}

	return instant.UTC().Format(time.RFC3339)
}
