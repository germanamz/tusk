# CLI Phase 1a: Arg Parsing

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the arg parsing foundation — `parseArgs`, `parsePriority`, and `parseDate` — as pure functions in `internal/tui/filter.go` with full test coverage.

**Architecture:** A single file `internal/tui/filter.go` containing stateless parsing functions. No dependencies on Cobra, services, or repositories — only Go standard library.

**Tech Stack:** Go standard library (`strings`, `strconv`, `fmt`, `time`).

---

### Task 1: Arg parser — `parseArgs`

**Files:**
- Create: `internal/tui/filter.go`
- Create: `internal/tui/filter_test.go`

This function takes a raw `[]string` from Cobra and classifies each arg into one of four buckets: title words, key:value fields, +tags, and -tags.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/filter_test.go`:

```go
package tui

import (
	"testing"
)

func TestParseArgs_TitleOnly(t *testing.T) {
	got := parseArgs([]string{"Implement", "auth", "middleware"})
	if got.Title != "Implement auth middleware" {
		t.Fatalf("expected title 'Implement auth middleware', got %q", got.Title)
	}
	if len(got.Fields) != 0 {
		t.Fatalf("expected no fields, got %d", len(got.Fields))
	}
	if len(got.Tags) != 0 {
		t.Fatalf("expected no tags, got %d", len(got.Tags))
	}
}

func TestParseArgs_KeyValuePairs(t *testing.T) {
	got := parseArgs([]string{"My", "task", "project:backend", "priority:3"})
	if got.Title != "My task" {
		t.Fatalf("expected title 'My task', got %q", got.Title)
	}
	if got.Fields["project"] != "backend" {
		t.Fatalf("expected project=backend, got %q", got.Fields["project"])
	}
	if got.Fields["priority"] != "3" {
		t.Fatalf("expected priority=3, got %q", got.Fields["priority"])
	}
}

func TestParseArgs_Tags(t *testing.T) {
	got := parseArgs([]string{"My", "task", "+api", "+frontend", "-docs"})
	if got.Title != "My task" {
		t.Fatalf("expected title 'My task', got %q", got.Title)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "api" || got.Tags[1] != "frontend" {
		t.Fatalf("expected tags [api frontend], got %v", got.Tags)
	}
	if len(got.ExclTags) != 1 || got.ExclTags[0] != "docs" {
		t.Fatalf("expected excl tags [docs], got %v", got.ExclTags)
	}
}

func TestParseArgs_AllMixed(t *testing.T) {
	got := parseArgs([]string{"Build", "the", "feature", "project:backend", "+api", "-docs", "priority:3"})
	if got.Title != "Build the feature" {
		t.Fatalf("expected title 'Build the feature', got %q", got.Title)
	}
	if got.Fields["project"] != "backend" {
		t.Fatalf("expected project=backend, got %q", got.Fields["project"])
	}
	if got.Fields["priority"] != "3" {
		t.Fatalf("expected priority=3, got %q", got.Fields["priority"])
	}
	if len(got.Tags) != 1 || got.Tags[0] != "api" {
		t.Fatalf("expected tags [api], got %v", got.Tags)
	}
	if len(got.ExclTags) != 1 || got.ExclTags[0] != "docs" {
		t.Fatalf("expected excl tags [docs], got %v", got.ExclTags)
	}
}

func TestParseArgs_Empty(t *testing.T) {
	got := parseArgs([]string{})
	if got.Title != "" {
		t.Fatalf("expected empty title, got %q", got.Title)
	}
	if len(got.Fields) != 0 {
		t.Fatalf("expected no fields, got %d", len(got.Fields))
	}
}

func TestParseArgs_ColonInValue(t *testing.T) {
	got := parseArgs([]string{"title:has:colons"})
	if got.Fields["title"] != "has:colons" {
		t.Fatalf("expected 'has:colons', got %q", got.Fields["title"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestParseArgs`
Expected: Compilation failure — `parseArgs` and `ParsedArgs` not defined.

- [ ] **Step 3: Implement parseArgs**

In `internal/tui/filter.go`:

```go
package tui

import "strings"

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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestParseArgs`
Expected: All 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/filter.go internal/tui/filter_test.go
git commit -m "feat(tui): implement arg parser with key:value, +tag, -tag support"
```

---

### Task 2: Priority parser

**Files:**
- Modify: `internal/tui/filter.go`
- Modify: `internal/tui/filter_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/filter_test.go`:

```go
func TestParsePriority_Numeric(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"0", 0},
		{"1", 1},
		{"2", 2},
		{"3", 3},
		{"4", 4},
	}
	for _, tt := range tests {
		got, err := parsePriority(tt.input)
		if err != nil {
			t.Fatalf("parsePriority(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parsePriority(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParsePriority_Named(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"none", 0},
		{"low", 1},
		{"medium", 2},
		{"high", 3},
		{"urgent", 4},
		{"None", 0},
		{"HIGH", 3},
	}
	for _, tt := range tests {
		got, err := parsePriority(tt.input)
		if err != nil {
			t.Fatalf("parsePriority(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parsePriority(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParsePriority_Invalid(t *testing.T) {
	for _, input := range []string{"5", "-1", "critical", "abc"} {
		_, err := parsePriority(input)
		if err == nil {
			t.Fatalf("parsePriority(%q): expected error", input)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestParsePriority`
Expected: Compilation failure — `parsePriority` not defined.

- [ ] **Step 3: Implement parsePriority**

Add `"fmt"` and `"strconv"` to the imports in `internal/tui/filter.go`, then append:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestParsePriority`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/filter.go internal/tui/filter_test.go
git commit -m "feat(tui): add priority parser with numeric and named values"
```

---

### Task 3: Date parser

**Files:**
- Modify: `internal/tui/filter.go`
- Modify: `internal/tui/filter_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/filter_test.go`:

```go
import "time"

func TestParseDate_RFC3339(t *testing.T) {
	got, err := parseDate("2026-04-10T15:30:00Z")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	want := time.Date(2026, 4, 10, 15, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_DateOnly(t *testing.T) {
	got, err := parseDate("2026-04-10")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	want := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Today(t *testing.T) {
	got, err := parseDate("today")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	now := time.Now().UTC()
	want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Tomorrow(t *testing.T) {
	got, err := parseDate("tomorrow")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	now := time.Now().UTC().AddDate(0, 0, 1)
	want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Weekday(t *testing.T) {
	// "monday" should return the next Monday from today
	got, err := parseDate("monday")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	if got.Weekday() != time.Monday {
		t.Fatalf("expected Monday, got %s", got.Weekday())
	}
	if got.Before(time.Now().UTC().Truncate(24 * time.Hour)) {
		t.Fatal("expected date in the future")
	}
}

func TestParseDate_Invalid(t *testing.T) {
	_, err := parseDate("notadate")
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestParseDate`
Expected: Compilation failure — `parseDate` not defined.

- [ ] **Step 3: Implement parseDate**

Add `"time"` to the imports in `internal/tui/filter.go`. Then append:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestParseDate`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/filter.go internal/tui/filter_test.go
git commit -m "feat(tui): add date parser with RFC3339, date-only, relative, weekday support"
```
