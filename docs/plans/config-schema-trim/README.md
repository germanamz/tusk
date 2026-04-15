# Config Schema Trim — Phased Plan

Initiative: remove `[projects.*]` / `[workflows.*]` from the TOML config schema. DB becomes the sole source of truth for projects and workflows. `tusk config show` still renders those sections for continuity, hydrated from the database.

Roadmap: `ROADMAP.md` → `v0.10` → `Initiative: Config Schema Trim`.

## Phase sequence

| # | Name | Prerequisites | Summary |
|---|------|---------------|---------|
| 1 | DB-hydrated config show + write rejection | base codebase | `config show`/`get` read projects/workflows from DB; `config set` and MCP `tusk_config_set` reject `projects.*`/`workflows.*` keys. Schema types still present. |
| 2 | Schema trim and sync removal | phase 1 | Delete `[projects.*]`/`[workflows.*]` from `default.toml`, remove config types, add legacy-section hard error in `config.Load`, delete `sqlite.SyncConfigToDB`, rewire `client.go` / `cmd/tusk/main.go` / fixtures. |
| 3 | Test fill-in, doc sweep, roadmap tick | phase 2 | New negative tests for legacy sections and `config set` rejection. Update `docs/programmatic-usage.md`. Tick roadmap boxes. Remove plan docs. |

Each phase leaves the system compile-clean, test-green, and independently shippable.

## Background: current DB seeding

Migrations already seed both the kanban workflow and the default project:

- `migrations/003_workflows.up.sql` inserts the kanban workflow row with id `00000000-0000-0000-0000-000000000000` and the canonical statuses/transitions JSON.
- `migrations/004_projects.up.sql` inserts the `_default` project row bound to kanban.
- `migrations/006_default_project_rename.up.sql` renames `_default` → `default`.

Consequence: a fresh database is fully populated *before* `sqlite.SyncConfigToDB` runs. `SyncConfigToDB` today is a no-op for the default config — its only active path is applying user-defined `[projects.foo]` settings on first run. Removing it in phase 2 has no effect on out-of-the-box behavior; the phase 2 legacy-section hard error ensures users with custom sections get a loud failure with guidance instead of silent data loss.
