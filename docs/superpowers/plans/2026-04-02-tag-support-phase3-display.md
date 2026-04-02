# Tag Support Phase 3: Tag Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Display tags in task list view, info view, and JSON output. Update the `info` command to fetch and show tags. Update the `tusk.md` roadmap.

**Architecture:** The render functions gain optional tag parameters. JSON output structs get a `Tags` field. The `info` command calls `tagSvc.GetTaskTags()` and passes results to the renderer. List view appends `+tag` suffixes to the title column.

**Tech Stack:** Go, `encoding/json`

**Spec:** `docs/superpowers/specs/2026-04-02-tag-support-design.md`

**Prerequisite:** Phase 2 (CLI wiring) must be complete.

---

### Task 1: Add tags to JSON output structs

**Files:**
- Modify: `internal/tui/render.go`

- [ ] **Step 1: Add Tags field to taskJSON and taskInfoJSON**

In `internal/tui/render.go`, add a `Tags` field to `taskJSON`:

Change:
```go
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
```

to:
```go
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
	Tags           []string       `json:"tags"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	UDA            map[string]any `json:"uda,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
}
```

Note: `Tags` uses `[]string` (not `omitempty`) so it always appears in JSON output as `[]` when empty, making it a predictable field for consumers.

- [ ] **Step 2: Update toTaskJSON to accept tags**

Change the `toTaskJSON` function signature and body. Change from:

```go
func toTaskJSON(t *domain.Task) taskJSON {
```

to:

```go
func toTaskJSON(t *domain.Task, tags []*domain.Tag) taskJSON {
```

Inside the function, after the existing field assignments and before the `return tj` statement, add:

```go
	tj.Tags = make([]string, len(tags))
	for i, tg := range tags {
		tj.Tags[i] = tg.Name
	}
```

- [ ] **Step 3: Update all callers of toTaskJSON to pass tags**

There are three places that call `toTaskJSON`. For now, pass `nil` (empty tags) to keep things compiling. We'll wire real tags in subsequent tasks.

In `renderTaskList` (the JSON branch), change:
```go
		items[i] = toTaskJSON(t)
```
to:
```go
		items[i] = toTaskJSON(t, nil)
```

In `renderTaskInfo` (the JSON branch), change:
```go
		info := taskInfoJSON{taskJSON: toTaskJSON(task)}
```
to:
```go
		info := taskInfoJSON{taskJSON: toTaskJSON(task, nil)}
```

In `renderMutationResult` (the JSON branch), change:
```go
		return enc.Encode(toTaskJSON(task))
```
to:
```go
		return enc.Encode(toTaskJSON(task, nil))
```

- [ ] **Step 4: Verify it compiles and tests pass**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./... -v`

Expected: Compiles and all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go
git commit -m "feat(tui): add Tags field to JSON output structs"
```

---

### Task 2: Show tags in list view (text + JSON)

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/commands.go`

The list view needs tags for each task. The `renderTaskList` function needs to receive tag data. We'll change its signature to accept a map of task ID to tag names, which the command layer populates.

- [ ] **Step 1: Update renderTaskList signature and implementation**

In `internal/tui/render.go`, add `"strings"` to the import block. The `domain` import (`"github.com/germanamz/tusk/internal/domain"`) should already be present since `render.go` uses `*domain.Task` and `*domain.Annotation`.

Change the `renderTaskList` function from:

```go
func renderTaskList(w io.Writer, tasks []*domain.Task, format string) error {
	if format == "json" {
		items := make([]taskJSON, len(tasks))
		for i, t := range tasks {
			items[i] = toTaskJSON(t, nil)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tasks) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n", "ID", "Status", "Pri", "Age", "Title"); err != nil {
		return err
	}
	for _, t := range tasks {
		if _, err := fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n",
			t.ShortID,
			t.Status,
			formatPriority(t.Priority),
			formatAge(t.CreatedAt),
			t.Title,
		); err != nil {
			return err
		}
	}
	return nil
}
```

to:

```go
func renderTaskList(w io.Writer, tasks []*domain.Task, taskTags map[string][]*domain.Tag, format string) error {
	if format == "json" {
		items := make([]taskJSON, len(tasks))
		for i, t := range tasks {
			items[i] = toTaskJSON(t, taskTags[t.ID.String()])
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tasks) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n", "ID", "Status", "Pri", "Age", "Title"); err != nil {
		return err
	}
	for _, t := range tasks {
		title := t.Title
		if tags, ok := taskTags[t.ID.String()]; ok && len(tags) > 0 {
			tagStrs := make([]string, len(tags))
			for i, tg := range tags {
				tagStrs[i] = "+" + tg.Name
			}
			title = title + "  " + strings.Join(tagStrs, " ")
		}
		if _, err := fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n",
			t.ShortID,
			t.Status,
			formatPriority(t.Priority),
			formatAge(t.CreatedAt),
			title,
		); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 2: Update runList to fetch tags and pass to renderTaskList**

In `internal/tui/commands.go`, update `runList`. Change from:

```go
	tasks, err := a.taskSvc.List(ctx, filter)
	if err != nil {
		return err
	}

	return renderTaskList(cmd.OutOrStdout(), tasks, a.format)
```

to:

```go
	tasks, err := a.taskSvc.List(ctx, filter)
	if err != nil {
		return err
	}

	// Fetch tags for each task
	taskTags := make(map[string][]*domain.Tag, len(tasks))
	for _, t := range tasks {
		tags, err := a.tagSvc.GetTaskTags(ctx, t.ID)
		if err != nil {
			return fmt.Errorf("loading tags for task %s: %w", t.ShortID, err)
		}
		if len(tags) > 0 {
			taskTags[t.ID.String()] = tags
		}
	}

	return renderTaskList(cmd.OutOrStdout(), tasks, taskTags, a.format)
```

You will also need to add `"github.com/germanamz/tusk/internal/domain"` to the imports in `commands.go` if not already present.

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./... -v`

Expected: Compiles and all tests PASS.

- [ ] **Step 4: Smoke test manually**

Run: `cd /Users/germanamz/projects/tusk && go run ./cmd/tusk --db /tmp/tusk-test-tags.db list`

Expected: Tasks with tags show them after the title, e.g.:
```
a3f8b2c1  pending   H   2d    Test tag task  +urgent +newfeature
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/commands.go
git commit -m "feat(tui): show tags in list view text and JSON output"
```

---

### Task 3: Show tags in info view

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Update renderTaskInfo to accept and display tags**

In `internal/tui/render.go`, change the `renderTaskInfo` function signature from:

```go
func renderTaskInfo(w io.Writer, task *domain.Task, annotations []*domain.Annotation, projectName string, format string) error {
```

to:

```go
func renderTaskInfo(w io.Writer, task *domain.Task, annotations []*domain.Annotation, tags []*domain.Tag, projectName string, format string) error {
```

Update the JSON branch to pass tags:

Change:
```go
		info := taskInfoJSON{taskJSON: toTaskJSON(task, nil)}
```
to:
```go
		info := taskInfoJSON{taskJSON: toTaskJSON(task, tags)}
```

In the text branch, after the "Priority:" line and before the "Description:" line, add a Tags section:

```go
	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Priority:", formatPriorityName(task.Priority)); err != nil {
		return err
	}

	if len(tags) > 0 {
		tagStrs := make([]string, len(tags))
		for i, tg := range tags {
			tagStrs[i] = "+" + tg.Name
		}
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Tags:", strings.Join(tagStrs, " ")); err != nil {
			return err
		}
	}
```

Make sure `"strings"` is in the import block (it was added in Task 2).

- [ ] **Step 2: Update runInfo to fetch tags and pass to renderTaskInfo**

In `internal/tui/commands.go`, update the `runInfo` function. Change from:

```go
	return renderTaskInfo(cmd.OutOrStdout(), task, annotations, projectName, a.format)
```

to:

```go
	// Fetch tags
	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	return renderTaskInfo(cmd.OutOrStdout(), task, annotations, tags, projectName, a.format)
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./... -v`

Expected: Compiles and all tests PASS.

- [ ] **Step 4: Smoke test manually**

Run: `cd /Users/germanamz/projects/tusk && go run ./cmd/tusk --db /tmp/tusk-test-tags.db info <SHORT_ID>`

Expected: Output includes a "Tags:" row:
```
ID:           a3f8b2c1
Title:        Test tag task
Status:       pending
Priority:     high
Tags:         +urgent +newfeature
...
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/commands.go
git commit -m "feat(tui): show tags in info view text and JSON output"
```

---

### Task 4: Update roadmap and run final verification

**Files:**
- Modify: `tusk.md`

- [ ] **Step 1: Update the v0.1 roadmap to mark tag support complete**

In `tusk.md`, change:
```
- [ ] Tag support: TagService, wire into CLI `add`/`modify`/`list`
```
to:
```
- [x] Tag support: TagService, wire into CLI `add`/`modify`/`list`
```

- [ ] **Step 2: Add tag subcommand to v0.2 roadmap**

In `tusk.md`, in the `### v0.2 — Relations and hierarchy` section, add at the end:

```
- [ ] `tusk tag` subcommand: create, list, delete, rename tags
```

- [ ] **Step 3: Add tag colors to v0.4 roadmap**

In `tusk.md`, in the `### v0.4 — Urgency and UX` section, add at the end:

```
- [ ] Tag colors: assign and display colors in TUI
```

- [ ] **Step 4: Run full test suite one final time**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`

Expected: All tests PASS.

Run: `cd /Users/germanamz/projects/tusk && go vet ./...`

Expected: No issues.

- [ ] **Step 5: Commit**

```bash
git add tusk.md
git commit -m "docs: update roadmap — mark tag support complete, add tag subcommand and colors items"
```

- [ ] **Step 6: Clean up test database**

Run: `rm -f /tmp/tusk-test-tags.db`
