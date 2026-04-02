# CLI Phase 1b: Output Rendering

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the output rendering functions — task list table, task info detail view, and mutation result — as pure functions in `internal/tui/render.go` with full test coverage.

**Architecture:** A single file `internal/tui/render.go` containing stateless functions that write to `io.Writer`. Depends only on `internal/domain` types and Go standard library. No Cobra or service layer dependencies.

**Tech Stack:** Go standard library (`encoding/json`, `fmt`, `io`, `time`), `internal/domain` types.

**Depends on:** Phase 1a (filter.go must exist so the package compiles).

---

### Task 1: Text rendering — task list table

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
			ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			ShortID:    "a3f8b2c1",
			ProjectID:  &projID,
			Status:     "pending",
			Priority:   0,
			Title:      "Test task",
			Version:    1,
			CreatedAt:  now,
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

### Task 2: Text rendering — task info detail view

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

### Task 3: Text rendering — mutation result

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
