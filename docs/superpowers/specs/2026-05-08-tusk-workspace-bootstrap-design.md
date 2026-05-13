---
type: spec
title: Tusk-on-Tusk Workspace Bootstrap — Design
---

# Tusk-on-Tusk Workspace Bootstrap

## Why

Plans 1a → 7.c.4 have built a working markdown vault + graph + retrieval engine. The natural next step is to dogfood it: turn the tusk repo itself into a tusk workspace so that **session continuity and reasoning** live as queryable nodes rather than ad-hoc handoff markdown.

Concretely, a future session should be able to ask the workspace via MCP:

- "What was the last shipped plan?"
- "Which spec did Plan 7.c.3 implement?"
- "What's the current status of `internal/behavior`?"
- "Show me handoffs from the last week."

…rather than re-reading static markdown each time.

## What lands

A new `dev` pack, an initialized workspace at the repo root, frontmatter on existing project-process documentation, and one status note per Go package.

### `dev` pack — `packs/dev.toml`

Four node types, one explicit edge type. Data-only (no workflow yet — `status` stays a plain enum to dodge the workflow-property-collision trap surfaced in 7.c.3).

```toml
[node-types.spec]
description = "A design specification for a subsystem or feature"
properties = []

[node-types.plan]
description = "An implementation plan that turns a spec into shipped work"
properties = [
    { name = "status",     type = "enum", values = ["draft", "in-progress", "shipped", "abandoned"] },
    { name = "pr",         type = "int" },
    { name = "shipped-at", type = "date" },
    { name = "implements", type = "list-of", item-type = "ref", to = "spec" },
]

[node-types.handoff]
description = "A session-continuity document bridging work between sessions"
properties = [
    { name = "session-date", type = "date" },
]

[node-types.package]
description = "A status summary for a Go package in the codebase"
properties = [
    { name = "import-path",     type = "string" },
    { name = "status",          type = "enum", values = ["stable", "in-flux", "experimental"] },
    { name = "last-touched-by", type = "ref", to = "plan" },
]

[edge-types.depends-on]
description = "Package-to-package import dependency"
from        = ["package"]
to          = ["package"]
cardinality = "many-to-many"
ordered     = false
acyclic     = true
inverse     = "dependency-of"
```

Ref properties (`plan.implements`, `package.last-touched-by`) auto-generate edge types of the same name (per 7.c.1). `depends-on` is declared explicitly to enforce `acyclic = true` on the package import DAG.

### Packs installed

- `dev` (custom, this spec) — installed via `file:///workspaces/tusk/packs/dev.toml`
- `vault` (built-in) — `note`, `decision`, plus the `references` edge that activates wikilink materialization workspace-wide
- `tags` (built-in) — universal `tag` node + `tagged` edge for taxonomy
- `kanban` (built-in) — `ticket` + `parent`/`blocks` edges + workflow; held in reserve for ticket-level tracking when a future plan adopts it

### Workspace shape

- Root: repo root. Manifest at `./tusk.toml`.
- Index: `./.tusk/tusk.db` (already gitignored).
- Ignore patterns added to `[workspace] ignore`:
  - Build / runtime: `bin/`, `dist/`, `.data/`, `.go-cache/`, `node_modules/`, `vendor/`, `*.test`, `tusk` (the binary), `bin/tusk`
  - Tooling: `.devcontainer/`, `.github/`, `.git/`, `.claude/`, `.superpowers/`, `.worktrees/`
  - Harness/human-targeted top-level docs: `CLAUDE.md`, `STYLE.md`, `PRODUCT.md`, `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`

### MCP at project scope

`.mcp.json` already exists but currently uses `args: ["mcp", "serve"]` — wrong subcommand. Fix to `args: ["mcp"]` (default stdio transport). Once Plan 8 starts, the assistant queries the workspace via MCP at session start instead of re-reading the handoff markdown.

## Migration

Files stay in place. We add YAML frontmatter to each. **Node IDs are auto-derived from file path** (the workspace-relative path, sans extension) — frontmatter does not carry an `id` field. Wikilinks resolve by full-path or by bare title within the target type, whichever is more readable.

Frontmatter shape per type:

```yaml
# spec
type: spec
title: <Spec Title>
```

```yaml
# plan
type: plan
title: <Plan Title>
status: shipped
pr: 363
shipped-at: 2026-05-08
implements:
  - "[[<spec-title>]]"
```

```yaml
# handoff
type: handoff
title: <Handoff Title>
session-date: 2026-05-08
```

```yaml
# note (vault)
type: note
title: <Note Title>
```

```yaml
# package (lives at docs/packages/<pkg>.md)
type: package
title: internal/<pkg> — <one-liner>
import-path: github.com/germanamz/tusk/internal/<pkg>
status: stable
last-touched-by: "[[<plan-title>]]"
```

All shipped plans get backfilled `status: shipped`, `pr: <n>`, `shipped-at: <date>`, and `implements:` refs (resolved by spec title) to the spec(s) they realized.

Per-package status notes live at `docs/packages/<pkg>.md` (16 internal + `cmd/tusk`). Sub-packages (e.g., `internal/behavior/workflow`) flatten to a single file with a hyphenated name (`docs/packages/behavior-workflow.md`). Initial pass: all status `stable`, `last-touched-by` set to the most recent plan that modified the package, brief 1-paragraph purpose statement + public surface bullets.

## Roll-out

Three commits on `v1` (no PR — `chore`-shaped, similar to `b407090`):

1. `chore(workspace): add dev pack and initialize tusk workspace` — pack file, `tusk init`, manifest config, `.mcp.json` fix.
2. `chore(workspace): migrate docs to tusk nodes` — frontmatter on all specs / plans / handoffs / dev-environment, this design doc included; `tusk reindex`.
3. `chore(workspace): seed package status notes` — 17 new `docs/packages/*.md`; `tusk reindex`.

Verification at each step: `make test && make lint`, `./tusk doctor` (zero issues expected), `./tusk node list --type=<X>` (counts match expectations).

## Out of scope

- Workflow on `plan.status` (validated state-machine transitions). Defer until usage shows we want it.
- `org` / `people` pack with `assignee` semantics. Future pack.
- Backfilling `depends-on` edges on the package nodes — initial pass leaves the dependency DAG empty. Could be derived from `go list -deps` later if useful.
- Lifting `dev` into a built-in pack alias (alongside `tags`/`kanban`/`vault`). It's experimental for now; promote if a second project adopts it.
- Migration of the 5 excluded top-level docs (`CLAUDE.md`, `STYLE.md`, `PRODUCT.md`, `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`). They're harness/human-targeted; not the assistant's primary surface.
