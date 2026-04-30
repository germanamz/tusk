package filter

import (
	"testing"
	"time"
)

func TestParseDate_RFC3339(test *testing.T) {
	got, err := parseDate("2026-04-10T15:30:00Z")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 4, 10, 15, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		test.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_DateOnly(test *testing.T) {
	got, err := parseDate("2026-04-10")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		test.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Today(test *testing.T) {
	got, err := parseDate("today")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	now := time.Now().UTC()
	want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		test.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Tomorrow(test *testing.T) {
	got, err := parseDate("tomorrow")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	now := time.Now().UTC().AddDate(0, 0, 1)
	want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		test.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Thisweek(test *testing.T) {
	got, err := parseDate("thisweek")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// thisweek should be the end of the current week (next Sunday 23:59:59)
	daysUntilSunday := int(time.Saturday - today.Weekday() + 1)
	if daysUntilSunday <= 0 {
		daysUntilSunday += 7
	}
	want := today.AddDate(0, 0, daysUntilSunday)
	if !got.Equal(want) {
		test.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Weekday(test *testing.T) {
	got, err := parseDate("monday")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	if got.Weekday() != time.Monday {
		test.Fatalf("expected Monday, got %s", got.Weekday())
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if got.Before(today) {
		test.Fatal("expected date in the future or today")
	}
}

func TestParseDate_Invalid(test *testing.T) {
	_, err := parseDate("notadate")
	if err == nil {
		test.Fatal("expected error for invalid date")
	}
}

func TestParseDateRange(test *testing.T) {
	start, end, err := parseDateRange("today..friday")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !start.Equal(today) {
		test.Fatalf("expected start=%v, got %v", today, start)
	}
	if end.Weekday() != time.Friday {
		test.Fatalf("expected end on Friday, got %s", end.Weekday())
	}
}

func TestParseDateRange_Absolute(test *testing.T) {
	start, end, err := parseDateRange("2026-04-01..2026-04-10")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	wantStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		test.Fatalf("expected start=%v, got %v", wantStart, start)
	}
	if !end.Equal(wantEnd) {
		test.Fatalf("expected end=%v, got %v", wantEnd, end)
	}
}
