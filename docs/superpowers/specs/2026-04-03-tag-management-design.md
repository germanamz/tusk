# Tag Management Design Spec

## Overview

Add a `tusk tag` subcommand group for dedicated tag CRUD operations: create, list, modify, delete, and rename. Currently tags are only created implicitly via `+tag` on `tusk add`/`modify` — this initiative gives tags first-class management.

## Scope

From ROADMAP.md v0.2 "Initiative: Tag Management":
- `tusk tag create <name>` — with optional `--color <hex>`
- `tusk tag list` — with `--color` filter and `--usage` task count
- `tusk tag modify <name>` — update color
- `tusk tag delete <name>` — fail if tag is assigned to tasks
- `tusk tag rename <old> <new>` — fail if new name already exists

## Design Decisions

1. **Explicit create vs. FindOrCreate**: `tusk tag create` fails if the tag already exists (unlike the implicit `FindOrCreate` used by `+tag` syntax). This gives the user clear feedback.
2. **No cascading delete**: Deleting a tag that is assigned to tasks returns `ErrTagInUse`. The user must remove the tag from tasks first via `tusk modify <task> -- -tag`.
3. **Rename collision**: Renaming to an existing name returns `ErrConflict`. No merge behavior.
4. **Color support**: The `Color *string` field exists in the domain and schema but is unused today. This initiative activates it via `--color` on create and modify.

## Repository Layer

### New methods on `TagRepository` interface

```go
GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error)
Update(ctx context.Context, tag *domain.Tag) error
Delete(ctx context.Context, id uuid.UUID) error
CountTasksByTagID(ctx context.Context, id uuid.UUID) (int, error)
ListWithUsage(ctx context.Context) ([]TagWithUsage, error)
```

### New type in `domain/tag.go`

```go
type TagWithUsage struct {
    Tag       domain.Tag
    TaskCount int
}
```

### SQLite implementation notes

- `Update`: `UPDATE tags SET name = ?, color = ? WHERE id = ?`
- `Delete`: `DELETE FROM tags WHERE id = ?` (no CASCADE check — service guards)
- `CountTasksByTagID`: `SELECT COUNT(*) FROM tag_assignments WHERE tag_id = ?`
- `ListWithUsage`: `SELECT t.id, t.name, t.color, COUNT(ta.task_id) FROM tags t LEFT JOIN tag_assignments ta ON t.id = ta.tag_id GROUP BY t.id`

## Service Layer

### New methods on `TagService`

```go
Create(ctx context.Context, name string, color *string) (*domain.Tag, error)
Delete(ctx context.Context, name string) error
Rename(ctx context.Context, oldName, newName string) error
Modify(ctx context.Context, name string, color *string) (*domain.Tag, error)
ListWithUsage(ctx context.Context) ([]TagWithUsage, error)
```

### New sentinel error

```go
// In domain/errors.go
var ErrTagInUse = errors.New("tag is assigned to tasks")
```

### Business logic

- **Create**: Trim/validate name, check `GetByName` returns `ErrNotFound` (else `ErrConflict`), then `repo.Create`.
- **Delete**: `GetByName` to resolve ID, `CountTasksByTagID > 0` returns `ErrTagInUse`, else `repo.Delete`.
- **Rename**: `GetByName(old)` to resolve, `GetByName(new)` must return `ErrNotFound` (else `ErrConflict`), update name via `repo.Update`.
- **Modify**: `GetByName` to resolve, set color, `repo.Update`.
- **ListWithUsage**: Thin delegation to `repo.ListWithUsage`.

## CLI Layer

### Command structure

New file `internal/tui/tag.go` with `buildTagCmd()` returning a `*cobra.Command` group, mirroring `project.go`.

| Command | Args | Flags | Handler |
|---|---|---|---|
| `tusk tag list` | none | `--color <filter>`, `--usage` | `runTagList` |
| `tusk tag create <name>` | 1 | `--color <hex>` | `runTagCreate` |
| `tusk tag modify <name>` | 1 | `--color <hex>` (empty string to clear) | `runTagModify` |
| `tusk tag delete <name>` | 1 | none | `runTagDelete` |
| `tusk tag rename <old> <new>` | 2 | none | `runTagRename` |

### `--color` filter on list

- `--color any` — tags with a color set
- `--color none` — tags without a color
- `--color <hex>` — tags matching that specific color
- Omitted — no filtering

### Rendering

New functions in `render.go`:

- `renderTagList(w, tags []TagWithUsage, showUsage bool, format)` — text table or JSON array
- `renderTagResult(w, action string, tag *domain.Tag, format)` — "Created tag foo" or JSON object

JSON output shape:
```json
{"id": "uuid", "name": "foo", "color": "#ff0000", "task_count": 3}
```

Text output:
```
Name   Color    Tasks
foo    #ff0000  3
bar    -        0
```

`task_count` column only shown when `--usage` is passed.

## E2E Tests

New file `tests/e2e/tag_management_test.go` with scenarios run across the standard 4-mode matrix:

| Scenario | What it validates |
|---|---|
| `tag_create` | Create succeeds; duplicate fails |
| `tag_create_with_color` | `--color` flag populates color field |
| `tag_list` | Created tags appear in list |
| `tag_list_with_usage` | `--usage` shows correct task count |
| `tag_list_filter_color` | `--color any`/`--color none` filtering works |
| `tag_modify_color` | Color is updated |
| `tag_modify_clear_color` | `--color ""` clears color |
| `tag_rename` | Name changes, old name gone |
| `tag_rename_conflict` | Rename to existing name fails |
| `tag_delete` | Tag removed from list |
| `tag_delete_in_use` | Delete fails when tag is assigned to tasks |

## Files to Change

| File | Change |
|---|---|
| `internal/domain/errors.go` | Add `ErrTagInUse` |
| `internal/domain/tag.go` | Add `TagWithUsage` struct |
| `internal/repository/tag.go` | Add 5 new interface methods |
| `internal/sqlite/tag.go` | Implement new methods |
| `internal/sqlite/tag_test.go` | Unit tests for new repo methods |
| `internal/service/tag.go` | Add 5 new service methods |
| `internal/service/tag_test.go` | Unit tests for new service methods |
| `internal/tui/tag.go` | New file: command group + handlers |
| `internal/tui/app.go` | Register `buildTagCmd()` |
| `internal/tui/render.go` | Add `renderTagList`, `renderTagResult` |
| `tests/e2e/tag_management_test.go` | New file: 11 E2E scenarios |

No new migrations needed — the existing schema supports all operations.
