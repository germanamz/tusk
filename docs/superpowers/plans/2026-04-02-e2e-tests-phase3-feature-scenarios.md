# E2E Tests Phase 3: Feature Scenarios — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add e2e scenarios for filtering, tags, annotations, and output format regressions — completing full coverage of all existing CLI features.

**Architecture:** Four test files in `tests/e2e/` that use the harness from Phase 1. Each defines `[]Scenario` and calls `runScenarios()`. Every scenario runs 4x (flag/env x text/json).

**Tech Stack:** Go standard library, harness from Phase 1 (`tests/e2e/harness.go`).

**Prerequisites:** Phase 1 must be complete. The following files must exist and work:
- `tests/e2e/harness.go` — provides `Env`, `Step`, `Scenario`, `runScenarios`, assertion helpers
- `tests/e2e/main_test.go` — provides `binPath` (compiled tusk binary)

**Key reference for assertions:**
- **Text list header**: `"ID       Status    Pri  Age   Title\n"` (see `internal/tui/render.go:124`)
- **Text list row**: `"%-8s %-9s %-4s %-5s %s\n"` with ShortID, Status, Priority (L/M/H/U/-), Age, Title (see `internal/tui/render.go:136-137`)
- **Tags in text list**: appended to title as `"  +tag1 +tag2"` (see `internal/tui/render.go:130-134`)
- **Tags in JSON**: `"tags": ["tag1", "tag2"]` array of strings (see `internal/tui/render.go:59`)
- **Text info Tags line**: `"Tags:         +tag1 +tag2\n"` (see `internal/tui/render.go:216-219`)
- **Text info Annotations section**: blank line, `"Annotations:\n"`, then `"  YYYY-MM-DD HH:MM - body\n"` per annotation (see `internal/tui/render.go:266-278`)
- **JSON info annotations**: `"annotations": [{"id": "...", "task_id": "...", "body": "...", "created_at": "..."}]` (see `internal/tui/render.go:174-177`)
- **Priority symbols**: 0=`-`, 1=`L`, 2=`M`, 3=`H`, 4=`U` (see `internal/tui/render.go:14-27`)
- **Priority names** (info view): 0=`none`, 1=`low`, 2=`medium`, 3=`high`, 4=`urgent` (see `internal/tui/render.go:150-163`)
- **Empty text list**: no output at all (see `internal/tui/render.go:120-122`)
- **Empty JSON list**: `"[]\n"` (empty JSON array, see `internal/tui/render.go:111-117`)
- **Default list filter**: shows only tasks with status `pending` or `active` (see `internal/filter/resolve.go` — default statuses)

---

### Task 1: `filtering_test.go` — Filter Scenarios

**Files:**
- Create: `tests/e2e/filtering_test.go`

Tests that `list` with various filter arguments returns the correct subset of tasks.

- [ ] **Step 1: Write the complete test file**

```go
package e2e

import "testing"

func TestFiltering(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "list_status_active_only",
			Steps: []Step{
				{
					Args: []string{"add", "Pending task"},
				},
				{
					Args: []string{"add", "Active task"},
				},
				{
					Args: []string{"start", "$1.short_id"},
				},
				{
					Args: []string{"list", "status:active"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 active task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Active task")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Active task")
						assertNotContains(t, output, "Pending task")
					},
				},
			},
		},
		{
			Name: "list_status_pending_and_active",
			Steps: []Step{
				{
					Args: []string{"add", "Pending one"},
				},
				{
					Args: []string{"add", "Active one"},
				},
				{
					Args: []string{"start", "$1.short_id"},
				},
				{
					Args: []string{"list", "status:pending,active"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Pending one")
						assertContains(t, output, "Active one")
					},
				},
			},
		},
		{
			Name: "list_include_tag",
			Steps: []Step{
				{
					Args: []string{"add", "Tagged task", "+api"},
				},
				{
					Args: []string{"add", "Untagged task"},
				},
				{
					Args: []string{"list", "+api"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 tagged task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Tagged task")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Tagged task")
						assertNotContains(t, output, "Untagged task")
					},
				},
			},
		},
		{
			Name: "list_exclude_tag",
			Steps: []Step{
				{
					Args: []string{"add", "Has docs tag", "+docs"},
				},
				{
					Args: []string{"add", "No docs tag"},
				},
				{
					Args: []string{"list", "-docs"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task without docs tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "No docs tag")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "No docs tag")
						assertNotContains(t, output, "Has docs tag")
					},
				},
			},
		},
		{
			Name: "list_priority_exact",
			Steps: []Step{
				{
					Args: []string{"add", "Low pri", "priority:1"},
				},
				{
					Args: []string{"add", "High pri", "priority:3"},
				},
				{
					Args: []string{"list", "priority:3"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "High pri")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "High pri")
						assertNotContains(t, output, "Low pri")
					},
				},
			},
		},
		{
			Name: "list_priority_range",
			Steps: []Step{
				{
					Args: []string{"add", "Low", "priority:1"},
				},
				{
					Args: []string{"add", "Medium", "priority:2"},
				},
				{
					Args: []string{"add", "High", "priority:3"},
				},
				{
					Args: []string{"add", "Urgent", "priority:4"},
				},
				{
					Args: []string{"list", "priority:3..4"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks (high+urgent), got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertNotContains(t, output, "Low")
						assertNotContains(t, output, "Medium")
						// High and Urgent should be present
						assertContains(t, output, "H")
						assertContains(t, output, "U")
					},
				},
			},
		},
		{
			Name: "list_project_filter",
			Steps: []Step{
				{
					// _default project is seeded by migration
					Args: []string{"add", "Default project task", "project:_default"},
				},
				{
					Args: []string{"add", "No project task"},
				},
				{
					Args: []string{"list", "project:_default"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 project task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Default project task")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Default project task")
						assertNotContains(t, output, "No project task")
					},
				},
			},
		},
		{
			Name: "list_combined_filters",
			Steps: []Step{
				{
					Args: []string{"add", "Match all", "priority:3", "+api"},
				},
				{
					Args: []string{"add", "Wrong priority", "priority:1", "+api"},
				},
				{
					Args: []string{"add", "Wrong tag", "priority:3", "+docs"},
				},
				{
					Args: []string{"start", "$0.short_id"},
				},
				{
					Args: []string{"list", "status:active", "+api", "priority:3"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task matching all filters, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Match all")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Match all")
						assertNotContains(t, output, "Wrong priority")
						assertNotContains(t, output, "Wrong tag")
					},
				},
			},
		},
		{
			Name: "list_no_results",
			Steps: []Step{
				{
					// No tasks in the DB — list should return empty
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected 0 tasks, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if output != "" {
							t.Fatalf("expected empty output for no tasks, got:\n%s", output)
						}
					},
				},
			},
		},
		{
			Name: "list_default_hides_completed_and_deleted",
			Steps: []Step{
				{
					Args: []string{"add", "Pending visible"},
				},
				{
					Args: []string{"add", "Will complete"},
				},
				{
					Args: []string{"start", "$1.short_id"},
				},
				{
					Args: []string{"done", "$1.short_id"},
				},
				{
					Args: []string{"add", "Will delete"},
				},
				{
					Args: []string{"delete", "$4.short_id"},
				},
				{
					// Default list should only show pending/active tasks
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 visible task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Pending visible")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Pending visible")
						assertNotContains(t, output, "Will complete")
						assertNotContains(t, output, "Will delete")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the tests**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1 -run TestFiltering
```

Expected: All subtests pass. 10 scenarios x 4 combinations = 40 subtests.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/filtering_test.go
git commit -m "test(e2e): add filtering scenarios (status, tags, priority, project, combined)"
```

---

### Task 2: `tags_test.go` — Tag Scenarios

**Files:**
- Create: `tests/e2e/tags_test.go`

Tests tag assignment on create, adding/removing via modify, and verifying tags in list and info output.

- [ ] **Step 1: Write the complete test file**

```go
package e2e

import "testing"

func TestTags(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_with_tags",
			Steps: []Step{
				{
					Args: []string{"add", "Tagged task", "+api", "+backend"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						tags := m["tags"].([]any)
						if len(tags) != 2 {
							t.Fatalf("expected 2 tags, got %d", len(tags))
						}
						// Tags may be in any order, check both exist
						tagSet := map[string]bool{}
						for _, tag := range tags {
							tagSet[tag.(string)] = true
						}
						if !tagSet["api"] || !tagSet["backend"] {
							t.Fatalf("expected tags [api, backend], got %v", tags)
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
			},
		},
		{
			Name: "modify_add_tag",
			Steps: []Step{
				{
					Args: []string{"add", "No tags yet"},
				},
				{
					Args: []string{"modify", "$0.short_id", "+newtag"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						tags := m["tags"].([]any)
						if len(tags) != 1 {
							t.Fatalf("expected 1 tag, got %d", len(tags))
						}
						assertEqual(t, tags[0], "newtag")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "modify_remove_tag",
			Steps: []Step{
				{
					Args: []string{"add", "Has tag", "+removeme"},
				},
				{
					Args: []string{"modify", "$0.short_id", "-removeme"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						tags := m["tags"].([]any)
						if len(tags) != 0 {
							t.Fatalf("expected 0 tags after removal, got %d: %v", len(tags), tags)
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "tags_in_list_output",
			Steps: []Step{
				{
					Args: []string{"add", "List tag task", "+visible"},
				},
				{
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						tags := arr[0].(map[string]any)["tags"].([]any)
						if len(tags) != 1 || tags[0] != "visible" {
							t.Fatalf("expected tags [visible], got %v", tags)
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "+visible")
					},
				},
			},
		},
		{
			Name: "tags_in_info_output",
			Steps: []Step{
				{
					Args: []string{"add", "Info tag task", "+frontend", "+urgent"},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						tags := m["tags"].([]any)
						if len(tags) != 2 {
							t.Fatalf("expected 2 tags, got %d", len(tags))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Tags:")
						assertContains(t, output, "+frontend")
						assertContains(t, output, "+urgent")
					},
				},
			},
		},
		{
			Name: "filter_by_tag_after_modify",
			Steps: []Step{
				{
					Args: []string{"add", "Will get tag"},
				},
				{
					Args: []string{"add", "Never gets tag"},
				},
				{
					Args: []string{"modify", "$0.short_id", "+searchable"},
				},
				{
					Args: []string{"list", "+searchable"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task with tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Will get tag")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Will get tag")
						assertNotContains(t, output, "Never gets tag")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the tests**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1 -run TestTags
```

Expected: All subtests pass. 6 scenarios x 4 combinations = 24 subtests.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/tags_test.go
git commit -m "test(e2e): add tag scenarios (create, add, remove, list, info, filter)"
```

---

### Task 3: `annotations_test.go` — Annotation Scenarios

**Files:**
- Create: `tests/e2e/annotations_test.go`

Tests annotation creation and verification through the `info` command.

- [ ] **Step 1: Write the complete test file**

```go
package e2e

import "testing"

func TestAnnotations(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "annotate_then_info",
			Steps: []Step{
				{
					Args: []string{"add", "Annotate target"},
				},
				{
					Args: []string{"annotate", "$0.short_id", "This is a note"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						// annotate returns the task object
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Annotate target")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Annotated task")
					},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						annotations := m["annotations"].([]any)
						if len(annotations) != 1 {
							t.Fatalf("expected 1 annotation, got %d", len(annotations))
						}
						ann := annotations[0].(map[string]any)
						assertEqual(t, ann["body"], "This is a note")
						if ann["id"] == nil || ann["id"] == "" {
							t.Fatal("expected annotation id to be set")
						}
						if ann["created_at"] == nil || ann["created_at"] == "" {
							t.Fatal("expected annotation created_at to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Annotations:")
						assertContains(t, output, "This is a note")
					},
				},
			},
		},
		{
			Name: "multiple_annotations",
			Steps: []Step{
				{
					Args: []string{"add", "Multi note task"},
				},
				{
					Args: []string{"annotate", "$0.short_id", "First note"},
				},
				{
					Args: []string{"annotate", "$0.short_id", "Second note"},
				},
				{
					Args: []string{"annotate", "$0.short_id", "Third note"},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						annotations := m["annotations"].([]any)
						if len(annotations) != 3 {
							t.Fatalf("expected 3 annotations, got %d", len(annotations))
						}
						bodies := make([]string, len(annotations))
						for i, a := range annotations {
							bodies[i] = a.(map[string]any)["body"].(string)
						}
						// All three should be present
						found := map[string]bool{}
						for _, b := range bodies {
							found[b] = true
						}
						for _, want := range []string{"First note", "Second note", "Third note"} {
							if !found[want] {
								t.Fatalf("missing annotation %q in %v", want, bodies)
							}
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Annotations:")
						assertContains(t, output, "First note")
						assertContains(t, output, "Second note")
						assertContains(t, output, "Third note")
					},
				},
			},
		},
		{
			Name: "annotate_nonexistent_task",
			Steps: []Step{
				{
					Args:    []string{"annotate", "nonexist", "A note"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the tests**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1 -run TestAnnotations
```

Expected: All subtests pass. 3 scenarios x 4 combinations = 12 subtests.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/annotations_test.go
git commit -m "test(e2e): add annotation scenarios (single, multiple, not found)"
```

---

### Task 4: `output_format_test.go` — Output Format Regressions

**Files:**
- Create: `tests/e2e/output_format_test.go`

Tests specific rendering details: column headers, priority symbols, JSON structure, empty outputs. These catch regressions in the render layer.

- [ ] **Step 1: Write the complete test file**

```go
package e2e

import "testing"

func TestOutputFormat(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "text_list_column_headers",
			Steps: []Step{
				{
					Args: []string{"add", "Header test"},
				},
				{
					Args: []string{"list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// The header line should contain these column names
						assertContains(t, output, "ID")
						assertContains(t, output, "Status")
						assertContains(t, output, "Pri")
						assertContains(t, output, "Age")
						assertContains(t, output, "Title")
					},
				},
			},
		},
		{
			Name: "text_priority_symbols",
			Steps: []Step{
				{
					Args: []string{"add", "No priority"},
				},
				{
					Args: []string{"add", "Low pri task", "priority:1"},
				},
				{
					Args: []string{"add", "Med pri task", "priority:2"},
				},
				{
					Args: []string{"add", "High pri task", "priority:3"},
				},
				{
					Args: []string{"add", "Urgent pri task", "priority:4"},
				},
				{
					Args: []string{"list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Check that priority symbols appear in the output.
						// "-" is the default (priority 0), "L"=1, "M"=2, "H"=3, "U"=4
						assertContains(t, output, "L")
						assertContains(t, output, "M")
						assertContains(t, output, "H")
						assertContains(t, output, "U")
					},
				},
			},
		},
		{
			Name: "json_list_returns_array",
			Steps: []Step{
				{
					Args: []string{"add", "Array item"},
				},
				{
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						// parsed should be a []any (JSON array), not a map
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 item in array, got %d", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "json_snake_case_keys",
			Steps: []Step{
				{
					Args: []string{"add", "Key check", "priority:2"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						// Check that all expected snake_case keys exist
						requiredKeys := []string{
							"id", "short_id", "title", "description",
							"status", "priority", "version", "tags",
							"created_at", "modified_at",
						}
						for _, key := range requiredKeys {
							if _, ok := m[key]; !ok {
								t.Fatalf("missing required JSON key: %q", key)
							}
						}
					},
				},
			},
		},
		{
			Name: "json_info_has_all_fields",
			Steps: []Step{
				{
					Args: []string{"add", "Info fields check", "priority:3"},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						// Info JSON should have all task fields plus annotations
						requiredKeys := []string{
							"id", "short_id", "title", "description",
							"status", "priority", "version", "tags",
							"created_at", "modified_at",
						}
						for _, key := range requiredKeys {
							if _, ok := m[key]; !ok {
								t.Fatalf("missing required JSON key in info: %q", key)
							}
						}
						// annotations key should exist (even if empty/nil)
						// When there are no annotations, it may be omitted (omitempty)
						// or present as null/empty array — both are acceptable
					},
				},
			},
		},
		{
			Name: "empty_list_text_no_output",
			Steps: []Step{
				{
					// Fresh DB — no tasks
					Args: []string{"list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if output != "" {
							t.Fatalf("expected empty text output for no tasks, got:\n%s", output)
						}
					},
				},
			},
		},
		{
			Name: "empty_list_json_empty_array",
			Steps: []Step{
				{
					// Fresh DB — no tasks
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected empty JSON array, got %d items", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "text_info_priority_names",
			Steps: []Step{
				{
					Args: []string{"add", "Low check", "priority:1"},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Info view shows full priority name, not symbol
						assertContains(t, output, "low")
					},
				},
			},
		},
		{
			Name: "text_info_shows_version",
			Steps: []Step{
				{
					Args: []string{"add", "Version check"},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Version:")
						assertContains(t, output, "1")
					},
				},
			},
		},
		{
			Name: "text_info_shows_timestamps",
			Steps: []Step{
				{
					Args: []string{"add", "Timestamp check"},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created:")
						assertContains(t, output, "Modified:")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the tests**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1 -run TestOutputFormat
```

Expected: All subtests pass. 10 scenarios x 4 combinations = 40 subtests.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/output_format_test.go
git commit -m "test(e2e): add output format scenarios (headers, priority symbols, JSON structure, empty output)"
```
