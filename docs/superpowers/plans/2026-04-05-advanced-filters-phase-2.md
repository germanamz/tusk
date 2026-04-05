# Phase 2: title/description Filter Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `title:` and `description:` filter fields to the CLI and MCP, enabling substring search on task titles and descriptions (e.g., `tusk list title:"auth middleware"`).

**Architecture:** Changes touch every layer of the filter pipeline: new validator in `parser.go`, new fields on `domain.TaskFilter`, new resolver cases, new SQL conditions in `buildFilter()`, and new MCP tool parameters. Each layer is a small, additive change.

**Tech Stack:** Go standard library only (no new dependencies).

**Prerequisites:** Phase 1 must be completed. Phase 1 introduces the quoted-aware lexer that allows `title:"multi word value"` to parse as a single `TokenField`.

---

## Inherits From

**Phase 1** modified these files:
- `internal/filter/token.go` — `Lex()` rewritten as character scanner with `scanQuoted()`. Quoted strings like `title:"fix the bug"` now lex as a single `TokenField` with `Value: "title:fix the bug"`.
- `internal/filter/token_test.go` — New test cases for quoted strings.
- `internal/filter/parser_test.go` — New test functions for quoted text through Parse.

The implementer can rely on:
- `title:"multi word"` lexes as `TokenField{Value: "title:multi word"}` — the quote characters are stripped, the key is `title`, the value is `multi word`.
- All existing filter expressions continue to work.

---

### Task 1: Add title and description to Domain TaskFilter

**Files:**
- Modify: `internal/domain/filter.go`
- Test: `internal/sqlite/task_test.go` (in Task 3)

- [ ] **Step 1: Add TitleContains and DescriptionContains fields**

In `internal/domain/filter.go`, add two fields after `WaitingOnly` (line 20) and before `UDA` (line 21):

```go
TitleContains       *string // substring match (case-insensitive)
DescriptionContains *string // substring match (case-insensitive)
```

The full struct should become:

```go
type TaskFilter struct {
	ProjectID           *string
	ParentID            *uuid.UUID
	RootID              *uuid.UUID // for tree: all descendants
	Statuses            []string   // OR match
	Tags                []string   // include
	ExcludeTags         []string   // exclude
	PriorityMin         *int
	PriorityMax         *int
	DueAfter            *time.Time
	DueBefore           *time.Time
	WaitingOnly         *bool             // if true, only tasks with wait_until in future
	TitleContains       *string           // substring match (case-insensitive)
	DescriptionContains *string           // substring match (case-insensitive)
	UDA                 map[string]string // filter by UDA key=value pairs (AND semantics); empty value = absent/empty match
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: PASS — new fields are additive, no existing code references them yet.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/filter.go
git commit -m "$(cat <<'EOF'
feat(domain): add TitleContains and DescriptionContains to TaskFilter
EOF
)"
```

---

### Task 2: Add Validator, Parser, and Resolver Support

**Files:**
- Modify: `internal/filter/parser.go` (add `title` and `description` to `fieldValidators`)
- Modify: `internal/filter/validators.go` (add `validateNonEmpty`)
- Modify: `internal/filter/resolve.go` (add `title` and `description` cases)
- Test: `internal/filter/parser_test.go`
- Test: `internal/filter/resolve_test.go`

- [ ] **Step 1: Write failing parser tests**

Add at the end of `internal/filter/parser_test.go`:

```go
func TestParse_TitleField(t *testing.T) {
	fs, errs := Parse(`title:"auth middleware"`)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	f, ok := fs.GetField("title")
	if !ok || f.Value != "auth middleware" {
		t.Fatalf("expected title=auth middleware, got %+v ok=%v", f, ok)
	}
}

func TestParse_DescriptionField(t *testing.T) {
	fs, errs := Parse(`description:"implement the feature"`)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	f, ok := fs.GetField("description")
	if !ok || f.Value != "implement the feature" {
		t.Fatalf("expected description=implement the feature, got %+v ok=%v", f, ok)
	}
}

func TestParse_TitleFieldEmpty(t *testing.T) {
	_, errs := Parse("title:")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for empty title, got %d: %v", len(errs), errs)
	}
}

func TestParse_DescriptionFieldEmpty(t *testing.T) {
	_, errs := Parse("description:")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for empty description, got %d: %v", len(errs), errs)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestParse_TitleField|TestParse_DescriptionField"`
Expected: FAIL — `title` and `description` are unknown fields.

- [ ] **Step 3: Add validateNonEmpty to validators.go**

Add at the end of `internal/filter/validators.go` (before the closing `ParsePriorityValue` function, around line 145):

```go
func validateNonEmpty(v string) error {
	if v == "" {
		return fmt.Errorf("value cannot be empty")
	}
	return nil
}
```

- [ ] **Step 4: Register title and description in fieldValidators**

In `internal/filter/parser.go`, add two entries to the `fieldValidators` map (after the `"waiting"` entry at line 18):

```go
"title":       validateNonEmpty,
"description": validateNonEmpty,
```

The full map becomes:

```go
var fieldValidators = map[string]func(string) error{
	"status":      validateStatus,
	"project":     validateProject,
	"priority":    validatePriority,
	"due":         validateDue,
	"parent":      validateShortID,
	"tree":        validateShortID,
	"waiting":     validateBool,
	"title":       validateNonEmpty,
	"description": validateNonEmpty,
}
```

- [ ] **Step 5: Run parser tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestParse_TitleField|TestParse_DescriptionField"`
Expected: ALL PASS.

- [ ] **Step 6: Write failing resolver tests**

Add at the end of `internal/filter/resolve_uda_test.go` (this file already uses `mockTaskLookup` which is sufficient — no DB needed):

```go
func TestResolve_TitleContains(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "title", Value: "auth middleware"},
		},
	}
	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.TitleContains == nil || *tf.TitleContains != "auth middleware" {
		t.Fatalf("expected TitleContains=auth middleware, got %v", tf.TitleContains)
	}
}

func TestResolve_DescriptionContains(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "description", Value: "implement feature"},
		},
	}
	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.DescriptionContains == nil || *tf.DescriptionContains != "implement feature" {
		t.Fatalf("expected DescriptionContains=implement feature, got %v", tf.DescriptionContains)
	}
}
```

- [ ] **Step 7: Run to verify failure**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestResolve_TitleContains|TestResolve_DescriptionContains"`
Expected: FAIL — resolver doesn't handle `title` or `description` fields.

- [ ] **Step 8: Add resolver cases**

In `internal/filter/resolve.go`, add two new cases inside the `switch field.Key` block (after the `case "waiting":` block at line 127, before the `default:` at line 129):

```go
		case "title":
			v := field.Value
			tf.TitleContains = &v

		case "description":
			v := field.Value
			tf.DescriptionContains = &v
```

- [ ] **Step 9: Run all resolver tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestResolve"`
Expected: ALL PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/filter/validators.go internal/filter/parser.go internal/filter/resolve.go internal/filter/parser_test.go internal/filter/resolve_uda_test.go
git commit -m "$(cat <<'EOF'
feat(filter): add title and description filter fields

Register title and description in fieldValidators with validateNonEmpty.
Resolve them to TitleContains and DescriptionContains on TaskFilter.
EOF
)"
```

---

### Task 3: Add SQL buildFilter Conditions

**Files:**
- Modify: `internal/sqlite/task.go` (add conditions in `buildFilter`)
- Test: `internal/sqlite/task_test.go`

- [ ] **Step 1: Write failing SQL-level tests**

Add at the end of `internal/sqlite/task_test.go`:

```go
func TestBuildFilter_TitleContains(t *testing.T) {
	v := "auth"
	filter := domain.TaskFilter{TitleContains: &v}
	_, where, args := buildFilter(filter)
	if !strings.Contains(where, "LOWER(title)") {
		t.Fatalf("expected LOWER(title) in WHERE clause, got %q", where)
	}
	if len(args) != 1 || args[0] != "auth" {
		t.Fatalf("expected args [auth], got %v", args)
	}
}

func TestBuildFilter_DescriptionContains(t *testing.T) {
	v := "implement"
	filter := domain.TaskFilter{DescriptionContains: &v}
	_, where, args := buildFilter(filter)
	if !strings.Contains(where, "LOWER(description)") {
		t.Fatalf("expected LOWER(description) in WHERE clause, got %q", where)
	}
	if len(args) != 1 || args[0] != "implement" {
		t.Fatalf("expected args [implement], got %v", args)
	}
}
```

You may need to add `"strings"` to the import block of `task_test.go` if not already present.

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/sqlite/ -run "TestBuildFilter_TitleContains|TestBuildFilter_DescriptionContains"`
Expected: FAIL — `buildFilter` doesn't handle these fields.

- [ ] **Step 3: Add title and description conditions to buildFilter**

In `internal/sqlite/task.go`, add these conditions inside `buildFilter()` after the `WaitingOnly` block (after line 248) and before the `UDA` block (line 249):

```go
	if filter.TitleContains != nil {
		conditions = append(conditions, "LOWER(title) LIKE '%' || LOWER(?) || '%'")
		args = append(args, *filter.TitleContains)
	}
	if filter.DescriptionContains != nil {
		conditions = append(conditions, "LOWER(description) LIKE '%' || LOWER(?) || '%'")
		args = append(args, *filter.DescriptionContains)
	}
```

- [ ] **Step 4: Run buildFilter tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/sqlite/ -run "TestBuildFilter"`
Expected: ALL PASS.

- [ ] **Step 5: Write a high-level List integration test**

Add at the end of `internal/sqlite/task_test.go`:

```go
func TestList_TitleContains(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t1.Title = "Implement auth middleware"
	mustCreateTask(t, repo, t1)

	t2 := newTestTask()
	t2.Title = "Write unit tests"
	mustCreateTask(t, repo, t2)

	v := "auth"
	tasks, err := repo.List(ctx, domain.TaskFilter{TitleContains: &v})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Title != "Implement auth middleware" {
		t.Fatalf("expected auth task, got %q", tasks[0].Title)
	}
}

func TestList_DescriptionContains(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t1.Title = "Task A"
	t1.Description = "This handles authentication"
	mustCreateTask(t, repo, t1)

	t2 := newTestTask()
	t2.Title = "Task B"
	t2.Description = "This handles logging"
	mustCreateTask(t, repo, t2)

	v := "authentication"
	tasks, err := repo.List(ctx, domain.TaskFilter{DescriptionContains: &v})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Title != "Task A" {
		t.Fatalf("expected Task A, got %q", tasks[0].Title)
	}
}
```

- [ ] **Step 6: Run List tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/sqlite/ -run "TestList_TitleContains|TestList_DescriptionContains"`
Expected: ALL PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/sqlite/task.go internal/sqlite/task_test.go
git commit -m "$(cat <<'EOF'
feat(sqlite): add title and description LIKE conditions to buildFilter
EOF
)"
```

---

### Task 4: Add MCP Tool Parameters

**Files:**
- Modify: `internal/mcp/server.go` (add tool parameter definitions)
- Modify: `internal/mcp/tools.go` (handle new parameters in `handleTaskList`)

- [ ] **Step 1: Add tool parameter definitions**

In `internal/mcp/server.go`, add two `mcp.WithString` calls inside the `tusk_task_list` tool definition. Add them after the `root` parameter (around line 249, before the closing `)`):

```go
			mcp.WithString("title",
				mcp.Description("Filter tasks whose title contains this substring (case-insensitive)"),
			),
			mcp.WithString("description",
				mcp.Description("Filter tasks whose description contains this substring (case-insensitive)"),
			),
```

- [ ] **Step 2: Handle new parameters in handleTaskList**

In `internal/mcp/tools.go`, add these blocks inside `handleTaskList` after the `root` handling block (after line 361, before the `tasks, err := s.taskSvc.List` call at line 363):

```go
	// Optional: title substring
	if title, err := request.RequireString("title"); err == nil {
		filter.TitleContains = &title
	}

	// Optional: description substring
	if desc, err := request.RequireString("description"); err == nil {
		filter.DescriptionContains = &desc
	}
```

- [ ] **Step 3: Verify compilation and all tests pass**

Run: `cd /Users/germanamz/projects/tusk && make test`
Expected: ALL PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go
git commit -m "$(cat <<'EOF'
feat(mcp): add title and description parameters to tusk_task_list
EOF
)"
```

---

### Task 5: Add E2E Tests

**Files:**
- Modify: `tests/e2e/filtering_test.go`

- [ ] **Step 1: Add E2E scenarios for title and description filtering**

Add these scenarios to the `scenarios` slice in `TestFiltering` (in `tests/e2e/filtering_test.go`):

```go
{
    Name: "filter_by_title",
    Steps: []Step{
        {Args: []string{"add", "Implement auth middleware"}},
        {Args: []string{"add", "Write unit tests"}},
        {
            Args: []string{"list", `title:"auth"`, "status:pending"},
            AssertJSON: func(t *testing.T, parsed any) {
                arr := jsonArray(t, parsed)
                if len(arr) != 1 {
                    t.Fatalf("expected 1 task, got %d", len(arr))
                }
                assertEqual(t, arr[0].(map[string]any)["title"], "Implement auth middleware")
            },
            AssertText: func(t *testing.T, output string) {
                assertContains(t, output, "auth middleware")
                assertNotContains(t, output, "unit tests")
            },
        },
    },
},
{
    Name: "filter_by_description",
    Steps: []Step{
        {Args: []string{"add", "Task A", "--description", "handles authentication"}},
        {Args: []string{"add", "Task B", "--description", "handles logging"}},
        {
            Args: []string{"list", `description:"authentication"`, "status:pending"},
            AssertJSON: func(t *testing.T, parsed any) {
                arr := jsonArray(t, parsed)
                if len(arr) != 1 {
                    t.Fatalf("expected 1 task, got %d", len(arr))
                }
                assertEqual(t, arr[0].(map[string]any)["title"], "Task A")
            },
            AssertText: func(t *testing.T, output string) {
                assertContains(t, output, "Task A")
                assertNotContains(t, output, "Task B")
            },
        },
    },
},
```

- [ ] **Step 2: Run E2E tests**

Run: `cd /Users/germanamz/projects/tusk && make test-e2e`
Expected: ALL PASS.

- [ ] **Step 3: Run all tests**

Run: `cd /Users/germanamz/projects/tusk && make test`
Expected: ALL PASS.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/filtering_test.go
git commit -m "$(cat <<'EOF'
test(e2e): add filtering by title and description scenarios
EOF
)"
```

---

## Changes Introduced

**Modified files:**
- `internal/domain/filter.go` — Added `TitleContains *string` and `DescriptionContains *string` to `TaskFilter`
- `internal/filter/validators.go` — Added `validateNonEmpty` function
- `internal/filter/parser.go` — Added `"title"` and `"description"` entries to `fieldValidators` map
- `internal/filter/resolve.go` — Added `case "title"` and `case "description"` in `Resolve()`
- `internal/sqlite/task.go` — Added `LOWER(title) LIKE` and `LOWER(description) LIKE` conditions in `buildFilter()`
- `internal/mcp/server.go` — Added `title` and `description` string parameters to `tusk_task_list` tool definition
- `internal/mcp/tools.go` — Added title/description parameter handling in `handleTaskList()`
- `internal/filter/parser_test.go` — Tests for title/description parse
- `internal/filter/resolve_uda_test.go` — Tests for title/description resolve
- `internal/sqlite/task_test.go` — Tests for buildFilter and List with title/description
- `tests/e2e/filtering_test.go` — E2E scenarios for title/description filtering

**No new files, dependencies, migrations, or environment variables.**

**No bridge code introduced.**

**User-visible behaviors preserved:**
- All existing filter expressions work unchanged
- All existing CLI commands behave identically
- All existing MCP tool parameters continue to work

**New user-visible behaviors:**
- `tusk list title:"search text"` — filters tasks by title substring (case-insensitive)
- `tusk list description:"search text"` — filters tasks by description substring (case-insensitive)
- `tusk_task_list` MCP tool accepts `title` and `description` string parameters
