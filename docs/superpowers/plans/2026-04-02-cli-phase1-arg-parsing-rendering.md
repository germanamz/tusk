# CLI Phase 1: Arg Parsing & Output Rendering

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pure-logic foundations for the CLI — arg parsing (`filter.go`) and output rendering (`render.go`) — with full test coverage before any Cobra wiring exists.

**Architecture:** Two files in `internal/tui/` containing stateless functions. `filter.go` parses CLI arg slices into structured data. `render.go` formats domain objects into text tables or JSON. Neither depends on Cobra or the service layer — they only depend on `internal/domain` types.

**Tech Stack:** Go standard library (`encoding/json`, `fmt`, `strings`, `time`, `io`, `os`), `internal/domain` types.

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

Append to `internal/tui/filter.go`:

```go
import (
	"fmt"
	"strconv"
)
```

Update the imports at the top, then add below `parseArgs`:

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

Add to the imports in `internal/tui/filter.go`: `"time"`. Then append:

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

---

### Task 4: Text rendering — task list table

**Files:**
- Create: `internal/tui/render.go`
- Create: `internal/tui/render_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/tui/render_test.go`:

```go
package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

func TestRenderTaskList_Text_SingleTask(t *testing.T) {
	now := time.Now().UTC()
	tasks := []*domain.Task{
		{
			ShortID:   "a3f8b2c1",
			Status:    "active",
			Priority:  3,
			Title:     "Implement auth middleware",
			CreatedAt: now.Add(-3 * 24 * time.Hour),
		},
	}

	var buf bytes.Buffer
	err := renderTaskList(&buf, tasks, "text")
	if err != nil {
		t.Fatalf("renderTaskList: %v", err)
	}

	out := buf.String()
	// Should have a header line and one data line
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + 1 task), got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "ID") {
		t.Fatalf("expected header with ID, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "a3f8b2c1") {
		t.Fatalf("expected short ID in output, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "active") {
		t.Fatalf("expected status in output, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "H") {
		t.Fatalf("expected priority H in output, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "3d") {
		t.Fatalf("expected age 3d in output, got %q", lines[1])
	}
}

func TestRenderTaskList_Text_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := renderTaskList(&buf, []*domain.Task{}, "text")
	if err != nil {
		t.Fatalf("renderTaskList: %v", err)
	}
	if buf.String() != "" {
		t.Fatalf("expected empty output for no tasks, got %q", buf.String())
	}
}

func TestRenderTaskList_JSON(t *testing.T) {
	projID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	now := time.Now().UTC().Truncate(time.Millisecond)
	tasks := []*domain.Task{
		{
			ID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			ShortID:   "a3f8b2c1",
			ProjectID: &projID,
			Status:    "pending",
			Priority:  0,
			Title:     "Test task",
			Version:   1,
			CreatedAt: now,
			ModifiedAt: now,
		},
	}

	var buf bytes.Buffer
	err := renderTaskList(&buf, tasks, "json")
	if err != nil {
		t.Fatalf("renderTaskList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"short_id"`) {
		t.Fatalf("expected snake_case JSON keys, got %s", out)
	}
	if !strings.Contains(out, `"a3f8b2c1"`) {
		t.Fatalf("expected short_id value in JSON, got %s", out)
	}
}

func TestFormatPriority(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "-"},
		{1, "L"},
		{2, "M"},
		{3, "H"},
		{4, "U"},
	}
	for _, tt := range tests {
		got := formatPriority(tt.input)
		if got != tt.want {
			t.Fatalf("formatPriority(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		created time.Time
		want    string
	}{
		{now.Add(-30 * time.Second), "0m"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-2 * 24 * time.Hour), "2d"},
		{now.Add(-15 * 24 * time.Hour), "2w"},
		{now.Add(-60 * 24 * time.Hour), "2mo"},
		{now.Add(-400 * 24 * time.Hour), "1y"},
	}
	for _, tt := range tests {
		got := formatAge(tt.created)
		if got != tt.want {
			t.Fatalf("formatAge(%v ago) = %q, want %q", now.Sub(tt.created), got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run "TestRenderTaskList|TestFormatPriority|TestFormatAge"`
Expected: Compilation failure — functions not defined.

- [ ] **Step 3: Implement render.go**

In `internal/tui/render.go`:

```go
package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/germanamz/tusk/internal/domain"
)

// formatPriority converts a priority int (0-4) to a single display character.
func formatPriority(p int) string {
	switch p {
	case 1:
		return "L"
	case 2:
		return "M"
	case 3:
		return "H"
	case 4:
		return "U"
	default:
		return "-"
	}
}

// formatAge converts a creation time to a human-readable relative age string.
func formatAge(created time.Time) string {
	d := time.Since(created)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// taskJSON is the JSON serialization format for a task.
// Field names use snake_case to match the domain model.
type taskJSON struct {
	ID             string         `json:"id"`
	ShortID        string         `json:"short_id"`
	ParentID       *string        `json:"parent_id,omitempty"`
	ProjectID      *string        `json:"project_id,omitempty"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       int            `json:"priority"`
	Version        int            `json:"version"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	UDA            map[string]any `json:"uda,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
}

func toTaskJSON(t *domain.Task) taskJSON {
	tj := taskJSON{
		ID:          t.ID.String(),
		ShortID:     t.ShortID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Version:     t.Version,
		UDA:         t.UDA,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  t.ModifiedAt.Format(time.RFC3339),
	}
	if t.ParentID != nil {
		s := t.ParentID.String()
		tj.ParentID = &s
	}
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		tj.ProjectID = &s
	}
	if t.DueAt != nil {
		s := t.DueAt.Format(time.RFC3339)
		tj.DueAt = &s
	}
	if t.WaitUntil != nil {
		s := t.WaitUntil.Format(time.RFC3339)
		tj.WaitUntil = &s
	}
	tj.RecurrenceRule = t.RecurrenceRule
	return tj
}

// renderTaskList writes a list of tasks to w in the given format.
// For "text", it renders a fixed-width table. For "json", it renders a JSON array.
// If the list is empty and format is "text", nothing is written.
func renderTaskList(w io.Writer, tasks []*domain.Task, format string) error {
	if format == "json" {
		items := make([]taskJSON, len(tasks))
		for i, t := range tasks {
			items[i] = toTaskJSON(t)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tasks) == 0 {
		return nil
	}

	fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n", "ID", "Status", "Pri", "Age", "Title")
	for _, t := range tasks {
		fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n",
			t.ShortID,
			t.Status,
			formatPriority(t.Priority),
			formatAge(t.CreatedAt),
			t.Title,
		)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run "TestRenderTaskList|TestFormatPriority|TestFormatAge"`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/render_test.go
git commit -m "feat(tui): implement task list rendering with text table and JSON formats"
```

---

### Task 5: Text rendering — task info detail view

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/render_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/render_test.go`:

```go
func TestRenderTaskInfo_Text_AllFields(t *testing.T) {
	projID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	parentID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	now := time.Now().UTC().Truncate(time.Millisecond)
	due := now.Add(24 * time.Hour)
	task := &domain.Task{
		ShortID:     "a3f8b2c1",
		Title:       "Implement auth",
		Description: "Build the auth middleware",
		Status:      "active",
		Priority:    3,
		ProjectID:   &projID,
		ParentID:    &parentID,
		DueAt:       &due,
		Version:     3,
		CreatedAt:   now,
		ModifiedAt:  now,
	}
	annotations := []*domain.Annotation{
		{Body: "Blocked by upstream", CreatedAt: now},
		{Body: "Unblocked", CreatedAt: now.Add(time.Hour)},
	}

	var buf bytes.Buffer
	err := renderTaskInfo(&buf, task, annotations, "text")
	if err != nil {
		t.Fatalf("renderTaskInfo: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"a3f8b2c1", "Implement auth", "active", "high", "Blocked by upstream", "Unblocked"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestRenderTaskInfo_Text_NullableFieldsOmitted(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ShortID:    "b7c9d4e2",
		Title:      "Simple task",
		Status:     "pending",
		Priority:   0,
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}

	var buf bytes.Buffer
	err := renderTaskInfo(&buf, task, nil, "text")
	if err != nil {
		t.Fatalf("renderTaskInfo: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Due:") {
		t.Fatalf("expected Due to be omitted, got:\n%s", out)
	}
	if strings.Contains(out, "Parent:") {
		t.Fatalf("expected Parent to be omitted, got:\n%s", out)
	}
	if strings.Contains(out, "Annotations:") {
		t.Fatalf("expected Annotations section to be omitted, got:\n%s", out)
	}
}

func TestRenderTaskInfo_JSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ShortID:    "a3f8b2c1",
		Title:      "Test",
		Status:     "pending",
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}

	var buf bytes.Buffer
	err := renderTaskInfo(&buf, task, nil, "json")
	if err != nil {
		t.Fatalf("renderTaskInfo: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"short_id"`) {
		t.Fatalf("expected snake_case JSON, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestRenderTaskInfo`
Expected: Compilation failure — `renderTaskInfo` not defined.

- [ ] **Step 3: Implement renderTaskInfo**

Append to `internal/tui/render.go`:

```go
// formatPriorityName converts a priority int to a full name for the info view.
func formatPriorityName(p int) string {
	switch p {
	case 1:
		return "low"
	case 2:
		return "medium"
	case 3:
		return "high"
	case 4:
		return "urgent"
	default:
		return "none"
	}
}

// renderTaskInfo writes a single task's detail view to w.
// For "text", it renders key-value pairs with optional annotations.
// For "json", it renders the task as a JSON object.
func renderTaskInfo(w io.Writer, task *domain.Task, annotations []*domain.Annotation, format string) error {
	if format == "json" {
		tj := toTaskJSON(task)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(tj)
	}

	fmt.Fprintf(w, "%-13s %s\n", "ID:", task.ShortID)
	fmt.Fprintf(w, "%-13s %s\n", "Title:", task.Title)
	fmt.Fprintf(w, "%-13s %s\n", "Status:", task.Status)
	fmt.Fprintf(w, "%-13s %s\n", "Priority:", formatPriorityName(task.Priority))

	if task.Description != "" {
		fmt.Fprintf(w, "%-13s %s\n", "Description:", task.Description)
	}
	if task.ProjectID != nil {
		fmt.Fprintf(w, "%-13s %s\n", "Project:", task.ProjectID.String())
	}
	if task.ParentID != nil {
		fmt.Fprintf(w, "%-13s %s\n", "Parent:", task.ParentID.String())
	}
	if task.DueAt != nil {
		fmt.Fprintf(w, "%-13s %s\n", "Due:", task.DueAt.Format("2006-01-02"))
	}
	if task.WaitUntil != nil {
		fmt.Fprintf(w, "%-13s %s\n", "Wait:", task.WaitUntil.Format("2006-01-02 15:04:05"))
	}
	if task.RecurrenceRule != nil {
		fmt.Fprintf(w, "%-13s %s\n", "Recurrence:", *task.RecurrenceRule)
	}

	fmt.Fprintf(w, "%-13s %s\n", "Created:", task.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "%-13s %s\n", "Modified:", task.ModifiedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "%-13s %d\n", "Version:", task.Version)

	if len(annotations) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Annotations:")
		for _, ann := range annotations {
			fmt.Fprintf(w, "  %s - %s\n", ann.CreatedAt.Format("2006-01-02 15:04"), ann.Body)
		}
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestRenderTaskInfo`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/render_test.go
git commit -m "feat(tui): implement task info detail rendering"
```

---

### Task 6: Text rendering — mutation result

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/render_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/render_test.go`:

```go
func TestRenderMutationResult_Text(t *testing.T) {
	task := &domain.Task{
		ShortID: "a3f8b2c1",
		Title:   "Test",
		Status:  "active",
		Version: 2,
	}

	var buf bytes.Buffer
	err := renderMutationResult(&buf, "Created", task, "text")
	if err != nil {
		t.Fatalf("renderMutationResult: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if out != "Created task a3f8b2c1" {
		t.Fatalf("expected 'Created task a3f8b2c1', got %q", out)
	}
}

func TestRenderMutationResult_JSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ShortID:    "a3f8b2c1",
		Title:      "Test",
		Status:     "active",
		Version:    2,
		CreatedAt:  now,
		ModifiedAt: now,
	}

	var buf bytes.Buffer
	err := renderMutationResult(&buf, "Created", task, "json")
	if err != nil {
		t.Fatalf("renderMutationResult: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"short_id"`) {
		t.Fatalf("expected JSON output with short_id, got:\n%s", out)
	}
	if !strings.Contains(out, `"version"`) {
		t.Fatalf("expected version in JSON output, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestRenderMutationResult`
Expected: Compilation failure — `renderMutationResult` not defined.

- [ ] **Step 3: Implement renderMutationResult**

Append to `internal/tui/render.go`:

```go
// renderMutationResult writes a one-line confirmation (text) or full task JSON.
// action is a past-tense verb like "Created", "Modified", "Started", etc.
func renderMutationResult(w io.Writer, action string, task *domain.Task, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(toTaskJSON(task))
	}
	fmt.Fprintf(w, "%s task %s\n", action, task.ShortID)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestRenderMutationResult`
Expected: All tests PASS.

- [ ] **Step 5: Run all tui tests to confirm nothing is broken**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v`
Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/render.go internal/tui/render_test.go
git commit -m "feat(tui): implement mutation result rendering"
```
