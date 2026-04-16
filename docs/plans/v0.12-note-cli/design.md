# Note CLI — Design

**Initiative:** Note CLI (v0.12)
**Status:** Drafted 2026-04-16. Supersedes no prior doc.

---

## Goal

Expose the v0.12 NoteService through the `tusk note` subcommand and add a `tusk player modify <id> note-window-size=<N>` command so the player-scoped window override documented in the roadmap is user-reachable. Service, repository, domain, migrations, and window resolution chain are already in place (see prior commits for Note Entity & Storage and Note Service initiatives). This initiative is CLI-only; MCP exposure is a separate roadmap initiative (Note MCP Tools) and is out of scope.

## Commands delivered

```
tusk note add <body> [project=<name>] [--task <short_id>] [meta.key=value...] [--player <id>]
tusk note list [project=<name>] [--task <short_id>] [--player <id>|--all-players]
               [--window <N>] [--since <7d|2w|24h>] [--archived]
tusk note archive <note_id_or_prefix> [--player <id>]

tusk player modify <id> note-window-size=<N>
tusk player modify <id> note-window-size=          # clears override
```

## Decisions

### D1. Note archive ID — UUID prefix match (min 8 chars)

Notes have no `short_id` column. Archive accepts either a full UUID or a prefix ≥ 8 characters. Prefix lookup is added on the `NoteRepository` interface and SQLite implementation. Ambiguous prefixes return an error listing matches.

Rationale: matches existing task short_id ergonomics without a schema change. A dedicated `short_id` column can be added in v0.13 if user feedback demands.

### D2. Metadata token shape — `meta.` prefix

Notes accept `meta.key=value` tokens, symmetric with task `uda.key=value`. Bare `key=value` that is not a reserved field (`project`) is rejected as unknown — typos on metadata keys surface loudly. An empty value on modify is not supported (notes are append-only; no modify command exists).

PRODUCT.md and ROADMAP.md updated in this same planning turn to reflect the `meta.` prefix.

### D3. Relative duration parsing — `filter/durations.go`

`--since` accepts relative durations (`7d`, `2w`, `24h`, `30m`). Go's `time.ParseDuration` handles hours/minutes/seconds but not days or weeks. A new `filter.ParseRelativeDuration` helper extends the grammar with `d` (day) and `w` (week) suffixes, stays in the `filter` package so MCP tools can reuse it, and rejects negative or zero values.

## Architecture

```
cobra cmd (internal/tui/note.go)
   │
   ├── resolve --player via existing a.ensurePlayer
   ├── resolve project by name via a.projectSvc.GetByName (default "_default")
   ├── resolve task by short_id via a.taskSvc.GetByShortID (when --task)
   ├── inline body via a.expandRefsWithState (reuses @file/@-/@@)
   ├── parse inline args via filter.Parse
   │      - project=<name>      → ProjectID
   │      - meta.<k>=<v>        → Metadata map
   │      - anything else       → error "unknown field"
   │
   ▼
service.NoteService.{Create,List,Archive}
   │
   ▼
repository.NoteRepository (existing + FindByIDPrefix)
```

`NoteService` is already constructed in `client.go` and exposed on `Client.Notes`. The `tui.App` struct does **not** yet hold a `noteSvc` field — Phase 3 adds the field, threads it through `tui.New`, and updates `cmd/tusk/main.go` to pass `client.Notes` in. MCP server construction does not need to change for this initiative.

`PlayerService` already has `UpdateNoteWindowSize` on the repository; Phase 2 adds a thin `SetNoteWindowSize` method on the service and exposes it via `tusk player modify`.

## Rendering

**Text per note:**

```
● 5f3e2d1c  agent-1  2h ago  task=a3f8b2c1  [archived]
  {glamour-rendered markdown body}
  meta.topic=auth  meta.type=discovery
```

- First 8 chars of UUID in color (matches short_id style).
- Relative timestamp via existing formatter (if present) or inline `time.Since` formatter — see Phase 3.
- `task=<short>` omitted when `TaskID` is nil.
- `[archived]` marker only when `--archived` shown and note is archived.
- Metadata line omitted when empty.
- Body rendered through `charm.land/glamour/v2` with the standard TUI style (dark/light auto).

**Text list:** reverse chronological (service already orders), one blank line between notes.

**JSON per note:**

```json
{
  "id": "5f3e2d1c-...",
  "project_id": "...",
  "player_id": "agent-1",
  "task_id": "a3f8b2c1-..." | null,
  "body": "...",
  "metadata": { "topic": "auth" },
  "created_at": "2026-04-16T10:15:00Z",
  "archived_at": null | "2026-04-16T10:30:00Z"
}
```

JSON list: array of the above.

## Phase breakdown

| Phase | Title | Prereqs | Ships |
|-------|-------|---------|-------|
| 1 | Shared helpers | none | `filter.ParseRelativeDuration`, `NoteRepository.FindByIDPrefix` — no user-visible change |
| 2 | Player modify | none | `tusk player modify <id> note-window-size=<N>` |
| 3 | Note add + archive | Phase 1 | `tusk note add`, `tusk note archive` |
| 4 | Note list + milestone close | Phase 1, Phase 3 | `tusk note list`, ROADMAP ticked |

Phase 1 and Phase 2 can run in parallel. Phase 3 starts after Phase 1. Phase 4 starts after Phase 3.

## Out of scope

- MCP tools for notes (separate roadmap initiative).
- `tusk note` on MCP field-restriction config (separate initiative).
- Note `short_id` column (deferred; UUID prefix match is sufficient).
- Status doc / release notes (milestone-close deliverables per auto-memory feedback).

## References

- Roadmap: `ROADMAP.md` v0.12, "Initiative: Note CLI" and "Story: Player window size preference"
- Product: `PRODUCT.md` Notes section + CLI note commands block
- Prior design: `docs/plans/v0.12-note-service/design.md` (service internals)
- Window resolution chain: `service/note.go` `resolveWindowSize`
