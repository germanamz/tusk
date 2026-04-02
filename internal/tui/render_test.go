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
