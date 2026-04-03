# Phase 3: CLI Commands & Wiring

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `link` and `unlink` CLI commands, enhance `info` to show relations, and wire `RelationService` into the application.

**Architecture:** New Cobra commands in the TUI layer call `RelationService` methods. The `App` struct gains a `relationSvc` field. `main.go` constructs and injects the service.

**Tech Stack:** Go, Cobra (CLI framework)

**Prerequisites:** Phase 1 and Phase 2 must be complete.

**Design spec:** `docs/superpowers/specs/2026-04-03-relation-service-design.md` (Section 4)

**Key files to understand before starting:**
- `internal/tui/app.go` — `App` struct (line 28), `New()` constructor (line 42), command registration pattern (line 65)
- `internal/tui/commands.go` — how commands are implemented (e.g., `runInfo` at line 151, `runAnnotate`)
- `internal/tui/render.go` — `renderMutationResult` (line 283), `renderTaskInfo` (line 179), `taskInfoJSON` (line 173)
- `cmd/tusk/main.go` — DI wiring (line 47)

---

### Task 1: Wire `RelationService` Into App and Main

**Files:**
- Modify: `internal/tui/app.go` (add `relationSvc` field and update constructor)
- Modify: `cmd/tusk/main.go` (construct RelationService and pass it)

- [ ] **Step 1: Add `relationSvc` field to `App` struct in `internal/tui/app.go`**

The current `App` struct (around line 28) looks like:

```go
type App struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	projectRepo ProjectLookup
	resolver    *filter.Resolver
	root        *cobra.Command
	format      string
	version     VersionInfo
}
```

Add the `relationSvc` field:

```go
type App struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	relationSvc *service.RelationService
	projectRepo ProjectLookup
	resolver    *filter.Resolver
	root        *cobra.Command
	format      string
	version     VersionInfo
}
```

- [ ] **Step 2: Update the `New()` constructor signature**

The current constructor (around line 42) is:

```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, projectRepo ProjectLookup, vi VersionInfo) *App {
```

Change it to:

```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, relationSvc *service.RelationService, projectRepo ProjectLookup, vi VersionInfo) *App {
```

And inside the function body, set the new field:

```go
a := &App{
	taskSvc:     taskSvc,
	tagSvc:      tagSvc,
	relationSvc: relationSvc,
	projectRepo: projectRepo,
	version:     vi,
}
```

- [ ] **Step 3: Update `cmd/tusk/main.go` to construct and pass `RelationService`**

In `main.go`, after the existing repo construction (around line 53), add:

```go
relationRepo := sqlite.NewRelationRepo(db)
```

After the existing service construction (around line 57), add:

```go
relationSvc := service.NewRelationService(relationRepo, taskRepo, store)
```

Update the `tui.New` call (around line 59) to pass `relationSvc`:

```go
app := tui.New(taskSvc, tagSvc, relationSvc, projectRepo, tui.VersionInfo{
```

The full wiring section should now look like:

```go
db := store.DB()
taskRepo := sqlite.NewTaskRepo(db)
annotationRepo := sqlite.NewAnnotationRepo(db)
projectRepo := sqlite.NewProjectRepo(db)
workflowRepo := sqlite.NewWorkflowRepo(db)
tagRepo := sqlite.NewTagRepo(db)
relationRepo := sqlite.NewRelationRepo(db)

workflowSvc := service.NewWorkflowService(workflowRepo)
taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)
tagSvc := service.NewTagService(tagRepo)
relationSvc := service.NewRelationService(relationRepo, taskRepo, store)

app := tui.New(taskSvc, tagSvc, relationSvc, projectRepo, tui.VersionInfo{
	Version: version,
	Commit:  commit,
	Date:    date,
})
```

You will need to add the `sqlite` import if not already present. It should already be there.

- [ ] **Step 4: Verify it compiles**

Run:
```bash
go build ./...
```
Expected: Compiles successfully.

- [ ] **Step 5: Run tests to catch any breakage**

Run:
```bash
make test
```
Expected: All tests pass. If any TUI tests construct `tui.New(...)` with the old signature, update them to pass `nil` for `relationSvc`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go cmd/tusk/main.go
git commit -m "feat(tui): wire RelationService into App and main

Add relationSvc field to App, update New() constructor, and
construct/inject RelationService in main.go."
```

---

### Task 2: Implement `link` Command

**Files:**
- Modify: `internal/tui/commands.go` (add `runLink` method)
- Modify: `internal/tui/app.go` (register the command)
- Modify: `internal/tui/render.go` (add `relationJSON` type and `renderLinkResult`)

- [ ] **Step 1: Add JSON type and render helper in `internal/tui/render.go`**

Add at the end of the file (after `renderMutationResult`):

```go
// relationJSON is the JSON serialization format for a relation.
type relationJSON struct {
	ID           string `json:"id"`
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
	CreatedAt    string `json:"created_at"`
}

// renderLinkResult writes a link confirmation (text) or full relation JSON.
func renderLinkResult(w io.Writer, rel *domain.Relation, sourceShortID, targetShortID, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(relationJSON{
			ID:           rel.ID.String(),
			SourceID:     rel.SourceID.String(),
			TargetID:     rel.TargetID.String(),
			RelationType: rel.RelationType,
			CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
		})
	}
	_, err := fmt.Fprintf(w, "Linked %s %s %s\n", sourceShortID, rel.RelationType, targetShortID)
	return err
}
```

- [ ] **Step 2: Add `runLink` method in `internal/tui/commands.go`**

Add at the end of the file:

```go
func (a *App) runLink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sourceShortID := args[0]
	relType := args[1]
	targetShortID := args[2]

	rel, err := a.relationSvc.Add(ctx, sourceShortID, targetShortID, relType)
	if err != nil {
		return fmt.Errorf("%s", formatRelationError(err, sourceShortID, targetShortID))
	}

	return renderLinkResult(cmd.OutOrStdout(), rel, sourceShortID, targetShortID, a.format)
}
```

Also add the `formatRelationError` helper in `commands.go` (near the existing `formatError` function):

```go
func formatRelationError(err error, sourceShortID, targetShortID string) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s or %s", sourceShortID, targetShortID)
	case errors.Is(err, domain.ErrCyclicBlock):
		return "relation would create a cycle in blocks graph"
	case errors.Is(err, domain.ErrDuplicateRelation):
		return "relation already exists"
	default:
		return err.Error()
	}
}
```

- [ ] **Step 3: Register the `link` command in `internal/tui/app.go`**

In the `New()` function, inside the `a.root.AddCommand(...)` block, add this command alongside the existing ones:

```go
&cobra.Command{
	Use:   "link <short_id> <relation_type> <short_id>",
	Short: "Create a relation between two tasks",
	Long:  `Create a typed relation. Types: blocks, relates_to, duplicates.`,
	Args:  cobra.ExactArgs(3),
	RunE:  a.runLink,
},
```

- [ ] **Step 4: Verify it compiles and manual smoke test**

Run:
```bash
go build ./... && echo "OK"
```
Expected: `OK`

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/commands.go internal/tui/render.go
git commit -m "feat(tui): add link command for creating task relations

Supports blocks, relates_to, and duplicates relation types.
Outputs text confirmation or JSON relation object."
```

---

### Task 3: Implement `unlink` Command

**Files:**
- Modify: `internal/tui/commands.go` (add `runUnlink`)
- Modify: `internal/tui/app.go` (register the command)

- [ ] **Step 1: Add `runUnlink` method in `internal/tui/commands.go`**

Add at the end of the file:

```go
func (a *App) runUnlink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sourceShortID := args[0]
	relType := args[1]
	targetShortID := args[2]

	if err := a.relationSvc.Remove(ctx, sourceShortID, targetShortID, relType); err != nil {
		return fmt.Errorf("%s", formatRelationError(err, sourceShortID, targetShortID))
	}

	if a.format == "json" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "{}")
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Unlinked %s %s %s\n", sourceShortID, relType, targetShortID)
	return err
}
```

- [ ] **Step 2: Register the `unlink` command in `internal/tui/app.go`**

In the `a.root.AddCommand(...)` block, add:

```go
&cobra.Command{
	Use:   "unlink <short_id> <relation_type> <short_id>",
	Short: "Remove a relation between two tasks",
	Long:  `Remove a typed relation. Types: blocks, relates_to, duplicates.`,
	Args:  cobra.ExactArgs(3),
	RunE:  a.runUnlink,
},
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./... && echo "OK"
```
Expected: `OK`

- [ ] **Step 4: Commit**

```bash
git add internal/tui/app.go internal/tui/commands.go
git commit -m "feat(tui): add unlink command for removing task relations

Mirrors link command structure. Returns text confirmation or
empty JSON object."
```

---

### Task 4: Enhance `info` Command to Show Relations

**Files:**
- Modify: `internal/tui/commands.go` (update `runInfo` to fetch relations)
- Modify: `internal/tui/render.go` (update `renderTaskInfo` and `taskInfoJSON` to include relations)

- [ ] **Step 1: Update `runInfo` in `internal/tui/commands.go`**

The current `runInfo` (around line 151) ends with:

```go
return renderTaskInfo(cmd.OutOrStdout(), task, annotations, tags, projectName, a.format)
```

Replace the entire `runInfo` method with:

```go
func (a *App) runInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	task, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	annotations, err := a.taskSvc.GetAnnotations(ctx, shortID)
	if err != nil {
		return fmt.Errorf("loading annotations: %w", err)
	}

	// Resolve project name for display
	var projectName string
	if task.ProjectID != nil {
		project, err := a.projectRepo.GetByID(ctx, *task.ProjectID)
		if err == nil {
			projectName = project.Name
		}
	}

	// Fetch tags
	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	// Fetch relations
	var relations []*domain.Relation
	if a.relationSvc != nil {
		relations, err = a.relationSvc.GetByTask(ctx, shortID)
		if err != nil {
			return fmt.Errorf("loading relations: %w", err)
		}
	}

	return renderTaskInfo(cmd.OutOrStdout(), task, annotations, tags, relations, projectName, a.format)
}
```

Note: we check `a.relationSvc != nil` for backward compatibility with tests that pass `nil`.

You will need to add the `domain` import to `commands.go` if not already present:

```go
"github.com/germanamz/tusk/internal/domain"
```

- [ ] **Step 2: Update `renderTaskInfo` signature and `taskInfoJSON` in `internal/tui/render.go`**

First, update `taskInfoJSON` (around line 173) to include relations:

```go
type taskInfoJSON struct {
	taskJSON
	Annotations []annotationJSON `json:"annotations,omitempty"`
	Relations   []relationJSON   `json:"relations,omitempty"`
}
```

Then update the `renderTaskInfo` function signature to accept relations. Replace the current function (starting around line 179) with:

```go
// renderTaskInfo writes a single task's detail view to w.
// For "text", it renders key-value pairs with optional annotations and relations.
// For "json", it renders the task as a JSON object including annotations and relations.
func renderTaskInfo(w io.Writer, task *domain.Task, annotations []*domain.Annotation, tags []*domain.Tag, relations []*domain.Relation, projectName string, format string) error {
	if format == "json" {
		info := taskInfoJSON{taskJSON: toTaskJSON(task, tags)}
		for _, ann := range annotations {
			info.Annotations = append(info.Annotations, annotationJSON{
				ID:        ann.ID.String(),
				TaskID:    ann.TaskID.String(),
				Body:      ann.Body,
				CreatedAt: ann.CreatedAt.Format(time.RFC3339),
			})
		}
		for _, rel := range relations {
			info.Relations = append(info.Relations, relationJSON{
				ID:           rel.ID.String(),
				SourceID:     rel.SourceID.String(),
				TargetID:     rel.TargetID.String(),
				RelationType: rel.RelationType,
				CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
			})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}
```

The text rendering section continues unchanged until after the annotations block (around line 278). After the annotations block, add the relations display:

```go
	if len(relations) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "Relations:"); err != nil {
			return err
		}
		for _, rel := range relations {
			// Determine direction label based on whether this task is source or target
			label := rel.RelationType
			relatedID := rel.TargetID.String()[:8]
			if rel.TargetID == task.ID {
				// This task is the target, show inverse label
				switch rel.RelationType {
				case "blocks":
					label = "blocked_by"
				case "relates_to":
					label = "related_to"
				case "duplicates":
					label = "duplicated_by"
				}
				relatedID = rel.SourceID.String()[:8]
			}
			if _, err := fmt.Fprintf(w, "  %-14s %s\n", label, relatedID); err != nil {
				return err
			}
		}
	}
```

Make sure to add the `domain` import in `render.go`:

```go
"github.com/germanamz/tusk/internal/domain"
```

Note: The `Relation` type is already imported as `domain.Relation` — verify the import path matches. The `domain` import should already be present since `render.go` references `*domain.Task` and `*domain.Annotation`.

- [ ] **Step 3: Fix any callers of the old `renderTaskInfo` signature**

The `renderTaskInfo` function now takes an additional `relations []*domain.Relation` parameter. Search the codebase for other callers:

Run:
```bash
grep -rn "renderTaskInfo" internal/tui/
```

If there are any test files or other callers using the old 6-argument signature, update them to pass `nil` for the relations parameter. The call should now be:

```go
renderTaskInfo(w, task, annotations, tags, nil, projectName, format)
```

or with actual relations:

```go
renderTaskInfo(w, task, annotations, tags, relations, projectName, format)
```

- [ ] **Step 4: Verify it compiles**

Run:
```bash
go build ./... && echo "OK"
```
Expected: `OK`

- [ ] **Step 5: Run all tests**

Run:
```bash
make test
```
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/commands.go internal/tui/render.go
git commit -m "feat(tui): show relations in info command output

Displays relations with direction-aware labels (blocks/blocked_by,
relates_to/related_to, duplicates/duplicated_by). Includes
relations in both text and JSON output formats."
```
