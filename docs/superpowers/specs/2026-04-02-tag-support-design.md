# Tag Support: TagService + CLI Wiring

**Date:** 2026-04-02
**Status:** Draft
**Roadmap item:** v0.1 — Tag support: TagService, wire into CLI `add`/`modify`/`list`

---

## Goal

Enable tag support in the CLI so users can assign, remove, filter, and view tags on tasks using the existing `+tag`/`-tag` syntax. Tags are auto-created on first use (TaskWarrior-style).

## Existing Foundation

The following are already implemented and tested:

- **Domain types:** `domain.Tag` (ID, Name, Color)
- **Repository interface:** `repository.TagRepository` — 6 methods (Create, GetByName, List, AssignToTask, RemoveFromTask, GetTaskTags)
- **SQLite implementation:** `sqlite.TagRepo` — all 6 methods with tests
- **Database schema:** `tags` and `tag_assignments` tables with indexes
- **Filter support:** `domain.TaskFilter.Tags` and `ExcludeTags` fields, fully implemented in `sqlite.TaskRepo.buildFilter()`
- **Argument parsing:** `tui.parseArgs()` already parses `+tag` into `ParsedArgs.Tags` and `-tag` into `ParsedArgs.ExclTags`

## Architecture

### New: TagService

A dedicated service encapsulating tag business logic. Follows the existing pattern where services own business rules and the CLI orchestrates calls across services.

**Location:** `internal/service/tag.go`

```go
type TagService struct {
    tagRepo repository.TagRepository
}

func NewTagService(tagRepo repository.TagRepository) *TagService

// FindOrCreate returns the existing tag or creates a new one.
func (s *TagService) FindOrCreate(ctx context.Context, name string) (*domain.Tag, error)

// AssignToTask finds-or-creates each tag by name and assigns them to the task.
func (s *TagService) AssignToTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error

// RemoveFromTask removes the named tags from the task. No-ops silently if not assigned.
func (s *TagService) RemoveFromTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error

// GetTaskTags returns all tags assigned to a task.
func (s *TagService) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error)

// List returns all tags.
func (s *TagService) List(ctx context.Context) ([]*domain.Tag, error)
```

**Validation:**
- `FindOrCreate` rejects empty tag names with a validation error.
- `AssignToTask` and `RemoveFromTask` accept empty slices as no-ops.

### DI Wiring

In `cmd/tusk/main.go`:

```go
tagRepo := sqlite.NewTagRepo(db)
tagSvc := service.NewTagService(tagRepo)
app := tui.New(taskSvc, tagSvc, projectRepo)
```

### App Changes

`tui.App` gains a `tagSvc *service.TagService` field. The `New()` constructor signature changes to accept it.

## CLI Command Changes

### `add` command

Remove the "tags not yet supported" error block. After `taskSvc.Create()` succeeds, call `tagSvc.AssignToTask(ctx, task.ID, parsed.Tags)`.

Flow:
1. Parse args (title, project, priority, due, parent, **tags**)
2. Create task via `taskSvc.Create()`
3. If `parsed.Tags` is non-empty, call `tagSvc.AssignToTask()`
4. Render output (now including tags)

### `modify` command

Remove the "tags not yet supported" error block. `+tag` adds tags, `-tag` (ExclTags) removes them.

Flow:
1. Parse args (fields to change, **+tags**, **-tags**)
2. Apply field modifications via `taskSvc.Update()`
3. If `parsed.Tags` is non-empty, call `tagSvc.AssignToTask()`
4. If `parsed.ExclTags` is non-empty, call `tagSvc.RemoveFromTask()`
5. Render output (now including tags)

### `list` command

Remove the "tag filtering not yet supported" error block in `buildTaskFilter()`. Pass `parsed.Tags` to `TaskFilter.Tags` and `parsed.ExclTags` to `TaskFilter.ExcludeTags`. The SQLite filtering layer already handles these correctly.

### `info` command

After fetching the task, call `tagSvc.GetTaskTags()` and include tags in the output.

## Display

### List view (text)

Append tags after the title, space-separated with `+` prefix:

```
a3f8b2c1  pending  H  2d  Implement auth  +api +backend
```

### Info view (text)

Add a "Tags" row:

```
Tags:       +api +backend
```

If no tags, omit the row.

### JSON output

Add a `"tags"` array of strings to both list items and info output:

```json
{
  "id": "a3f8b2c1",
  "title": "Implement auth",
  "tags": ["api", "backend"],
  ...
}
```

## Error Handling

- `FindOrCreate` is idempotent — safe to call repeatedly with the same name.
- `AssignToTask` uses `INSERT OR IGNORE` at the SQLite level — duplicate assignments are no-ops.
- `RemoveFromTask` silently succeeds if the tag wasn't assigned (no error surfaced to user). If the tag name doesn't exist at all, it's also a silent no-op (nothing to remove).
- Empty tag name → validation error from `FindOrCreate`.

## Testing

### Unit tests: `internal/service/tag_test.go`

- `FindOrCreate` with new tag name → creates and returns tag
- `FindOrCreate` with existing tag name → returns existing tag
- `FindOrCreate` with empty name → returns error
- `AssignToTask` with multiple tag names → all assigned
- `AssignToTask` with empty slice → no-op
- `RemoveFromTask` → delegates correctly

### Smoke e2e tests

CLI e2e testing is a separate roadmap milestone. When that framework is built, these scenarios should be covered:

- `tusk add "task" +bug +urgent` → task created with both tags
- `tusk list +bug` → filters to tagged tasks only
- `tusk list -bug` → excludes tagged tasks
- `tusk modify <id> +new -old` → adds and removes tags
- `tusk info <id>` → shows tags in output

## Out of Scope

The following are deferred:

- **`tusk tag` subcommand** — explicit tag management (create, list, delete, rename) → **v0.2**
- **Tag colors** — assign and display colors in TUI → **v0.4**
- Tag-based urgency scoring → **v0.4**

## Roadmap Update

Add to `tusk.md`:

- **v0.2:** `tusk tag` subcommand (create, list, delete, rename tags)
- **v0.4:** Tag colors (assign and display colors in TUI)
