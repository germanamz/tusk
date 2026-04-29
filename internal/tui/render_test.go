package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func TestRenderTaskList_Text_SingleTask(test *testing.T) {
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
	renderer := NewRenderer(&buf, "text", false, nil)
	err := renderer.renderTaskList(tasks, nil)

	if err != nil {
		test.Fatalf("renderTaskList: %v", err)
	}

	out := buf.String()
	// Should have a header line and one data line
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		test.Fatalf("expected 2 lines (header + 1 task), got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "ID") {
		test.Fatalf("expected header with ID, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "a3f8b2c1") {
		test.Fatalf("expected short ID in output, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "active") {
		test.Fatalf("expected status in output, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "H") {
		test.Fatalf("expected priority H in output, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "3d") {
		test.Fatalf("expected age 3d in output, got %q", lines[1])
	}
}

func TestRenderTaskList_Text_Empty(test *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "text", false, nil)
	err := renderer.renderTaskList([]*domain.Task{}, nil)

	if err != nil {
		test.Fatalf("renderTaskList: %v", err)
	}

	if buf.String() != "" {
		test.Fatalf("expected empty output for no tasks, got %q", buf.String())
	}
}

func TestRenderTaskList_JSON(test *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	tasks := []*domain.Task{
		{
			ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			ShortID:    "a3f8b2c1",
			ProjectID:  domain.DefaultProjectUUID,
			Status:     "pending",
			Priority:   0,
			Title:      "Test task",
			Version:    1,
			CreatedAt:  now,
			ModifiedAt: now,
		},
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "json", false, nil)
	err := renderer.renderTaskList(tasks, nil)

	if err != nil {
		test.Fatalf("renderTaskList: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"short_id"`) {
		test.Fatalf("expected snake_case JSON keys, got %s", out)
	}
	if !strings.Contains(out, `"a3f8b2c1"`) {
		test.Fatalf("expected short_id value in JSON, got %s", out)
	}
	if strings.Contains(out, `"level"`) {
		test.Fatalf("level should be omitted when unset, got %s", out)
	}
}

func TestRenderTaskList_JSON_WithLevel(test *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	level := "story"
	tasks := []*domain.Task{
		{
			ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			ShortID:    "a3f8b2c1",
			ProjectID:  domain.DefaultProjectUUID,
			Status:     "pending",
			Priority:   0,
			Title:      "Test task",
			Level:      &level,
			Version:    1,
			CreatedAt:  now,
			ModifiedAt: now,
		},
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "json", false, nil)
	if err := renderer.renderTaskList(tasks, nil); err != nil {
		test.Fatalf("renderTaskList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"level": "story"`) {
		test.Fatalf("expected level 'story' in JSON, got %s", out)
	}
}

func TestRenderTaskList_Text_WithTags(test *testing.T) {
	now := time.Now().UTC()
	taskID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tasks := []*domain.Task{
		{
			ID:        taskID,
			ShortID:   "a3f8b2c1",
			Status:    "pending",
			Priority:  2,
			Title:     "Build API",
			CreatedAt: now.Add(-2 * 24 * time.Hour),
		},
	}
	taskTags := map[string][]*domain.Tag{
		taskID.String(): {
			{Name: "api"},
			{Name: "backend"},
		},
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "text", false, nil)
	err := renderer.renderTaskList(tasks, taskTags)

	if err != nil {
		test.Fatalf("renderTaskList: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "+api") {
		test.Fatalf("expected +api in output, got:\n%s", out)
	}
	if !strings.Contains(out, "+backend") {
		test.Fatalf("expected +backend in output, got:\n%s", out)
	}
}

func TestRenderTaskList_JSON_WithTags(test *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	taskID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tasks := []*domain.Task{
		{
			ID:         taskID,
			ShortID:    "a3f8b2c1",
			Status:     "pending",
			Title:      "Build API",
			Version:    1,
			CreatedAt:  now,
			ModifiedAt: now,
		},
	}
	taskTags := map[string][]*domain.Tag{
		taskID.String(): {
			{Name: "api"},
		},
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "json", false, nil)
	err := renderer.renderTaskList(tasks, taskTags)

	if err != nil {
		test.Fatalf("renderTaskList: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"api"`) {
		test.Fatalf("expected tag 'api' in JSON output, got:\n%s", out)
	}
}

func TestRenderTaskInfo_Text_WithTags(test *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ShortID:    "a3f8b2c1",
		Title:      "Build API",
		Status:     "active",
		Priority:   2,
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}
	tags := []*domain.Tag{
		{Name: "api"},
		{Name: "backend"},
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "text", false, nil)
	err := renderer.renderTaskInfo(task, nil, tags, nil)

	if err != nil {
		test.Fatalf("renderTaskInfo: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Tags:") {
		test.Fatalf("expected Tags: row in output, got:\n%s", out)
	}
	if !strings.Contains(out, "+api") {
		test.Fatalf("expected +api in output, got:\n%s", out)
	}
	if !strings.Contains(out, "+backend") {
		test.Fatalf("expected +backend in output, got:\n%s", out)
	}
}

func TestRenderTaskInfo_JSON_WithTags(test *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ShortID:    "a3f8b2c1",
		Title:      "Build API",
		Status:     "active",
		Priority:   2,
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}
	tags := []*domain.Tag{
		{Name: "api"},
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "json", false, nil)
	err := renderer.renderTaskInfo(task, nil, tags, nil)

	if err != nil {
		test.Fatalf("renderTaskInfo: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"tags"`) {
		test.Fatalf("expected tags field in JSON output, got:\n%s", out)
	}
	if !strings.Contains(out, `"api"`) {
		test.Fatalf("expected tag 'api' in JSON output, got:\n%s", out)
	}
}

func TestRenderMutationResult_JSON_WithTags(test *testing.T) {
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
	tags := []*domain.Tag{
		{Name: "bug"},
		{Name: "urgent"},
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "json", false, nil)
	err := renderer.renderMutationResult("Created", task, tags)

	if err != nil {
		test.Fatalf("renderMutationResult: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"bug"`) {
		test.Fatalf("expected tag 'bug' in JSON output, got:\n%s", out)
	}
	if !strings.Contains(out, `"urgent"`) {
		test.Fatalf("expected tag 'urgent' in JSON output, got:\n%s", out)
	}
}

func TestFormatPriority(test *testing.T) {
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
			test.Fatalf("formatPriority(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatAge(test *testing.T) {
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
			test.Fatalf("formatAge(%v ago) = %q, want %q", now.Sub(tt.created), got, tt.want)
		}
	}
}

func TestRenderTaskInfo_Text_AllFields(test *testing.T) {
	parentID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	now := time.Now().UTC().Truncate(time.Millisecond)
	due := now.Add(24 * time.Hour)
	task := &domain.Task{
		ShortID:     "a3f8b2c1",
		Title:       "Implement auth",
		Description: "Build the auth middleware",
		Status:      "active",
		Priority:    3,
		ProjectID:   domain.DefaultProjectUUID,
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
	renderer := NewRenderer(&buf, "text", false, nil)
	err := renderer.renderTaskInfo(task, annotations, nil, nil)

	if err != nil {
		test.Fatalf("renderTaskInfo: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"a3f8b2c1", "Implement auth", "active", "high", "Blocked by upstream", "Unblocked"} {
		if !strings.Contains(out, want) {
			test.Fatalf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestRenderTaskInfo_Text_OrderPresent(test *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	order := 2.5
	task := &domain.Task{
		ShortID:    "a3f8b2c1",
		Title:      "Ordered",
		Status:     "pending",
		Priority:   1,
		Order:      &order,
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "text", false, nil)
	if err := renderer.renderTaskInfo(task, nil, nil, nil); err != nil {
		test.Fatalf("renderTaskInfo: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Order:") {
		test.Fatalf("expected 'Order:' label in output, got:\n%s", out)
	}
	if !strings.Contains(out, "2.5") {
		test.Fatalf("expected order value 2.5 in output, got:\n%s", out)
	}
}

func TestRenderTaskInfo_Text_OrderOmittedWhenNil(test *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ShortID:    "a3f8b2c1",
		Title:      "Unordered",
		Status:     "pending",
		Priority:   1,
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "text", false, nil)
	if err := renderer.renderTaskInfo(task, nil, nil, nil); err != nil {
		test.Fatalf("renderTaskInfo: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Order:") {
		test.Fatalf("expected 'Order:' line to be omitted when order is nil, got:\n%s", out)
	}
}

func TestRenderTaskInfo_Text_NullableFieldsOmitted(test *testing.T) {
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
	renderer := NewRenderer(&buf, "text", false, nil)
	err := renderer.renderTaskInfo(task, nil, nil, nil)

	if err != nil {
		test.Fatalf("renderTaskInfo: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "Due:") {
		test.Fatalf("expected Due to be omitted, got:\n%s", out)
	}
	if strings.Contains(out, "Parent:") {
		test.Fatalf("expected Parent to be omitted, got:\n%s", out)
	}
	if strings.Contains(out, "Annotations:") {
		test.Fatalf("expected Annotations section to be omitted, got:\n%s", out)
	}
}

func TestRenderTaskInfo_JSON(test *testing.T) {
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
	renderer := NewRenderer(&buf, "json", false, nil)
	err := renderer.renderTaskInfo(task, nil, nil, nil)

	if err != nil {
		test.Fatalf("renderTaskInfo: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"short_id"`) {
		test.Fatalf("expected snake_case JSON, got:\n%s", out)
	}
}

func TestRenderMutationResult_Text(test *testing.T) {
	task := &domain.Task{
		ShortID: "a3f8b2c1",
		Title:   "Test",
		Status:  "active",
		Version: 2,
	}

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "text", false, nil)
	err := renderer.renderMutationResult("Created", task, nil)

	if err != nil {
		test.Fatalf("renderMutationResult: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Created task a3f8b2c1" {
		test.Fatalf("expected 'Created task a3f8b2c1', got %q", out)
	}
}

func TestRenderMutationResult_JSON(test *testing.T) {
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
	renderer := NewRenderer(&buf, "json", false, nil)
	err := renderer.renderMutationResult("Created", task, nil)

	if err != nil {
		test.Fatalf("renderMutationResult: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"short_id"`) {
		test.Fatalf("expected JSON output with short_id, got:\n%s", out)
	}
	if !strings.Contains(out, `"version"`) {
		test.Fatalf("expected version in JSON output, got:\n%s", out)
	}
}
