---
type: spec
title: Tusk v1 Rebuild
---

# Tusk v1 — Agent Brain Rebuild

- **Status:** Draft
- **Date:** 2026-05-05
- **Author:** German Meza
- **Successor of:** Tusk v0.x (last release: `v0.14.0`)
- **Supersedes:** any in-flight v0.15 design notes (Workspace Mode + Sync)

---

## 1. Summary

Tusk v1 is a ground-up rewrite that recasts tusk from a concurrent-safe task manager into a **local-first agent brain**: a markdown vault with a smart, schema-validated, semantically-indexed graph layered on top.

Every captured concept — a ticket, a note, a decision, a meeting, a tag — is a node in a typed graph. The user's filesystem is the source of truth. Git is the history and the sync mechanism. Tusk is an indexer, schema enforcer, and retrieval engine that runs locally as a single binary, exposes a CLI and an MCP server, and never owns the data it indexes.

Headline capability: an agent (or human) issues one query and gets back the most semantically- and structurally-relevant nodes regardless of type, drawn from a vault that any other tool (vim, Obsidian, ripgrep, git) can also read.

## 2. Motivation

The current tusk v0.x model fragments retrieval across multiple sources of truth: tasks, notes, annotations, tags, and players each live in their own table with their own attachment shape. An agent that needs context for a piece of work has to know *which* surface to query for *which* kind of information, and orchestrate across them. This produces long research iterations and brittle prompts.

The fix proposed here is structural, not additive:

1. **Collapse all entity types into a generic Node + Edge substrate**, with a workspace-level manifest declaring the types, properties, and edge legality.
2. **Move from database-as-source-of-truth to filesystem-as-source-of-truth**, so content lives as markdown files that any tool can read and git can version.
3. **Layer a unified semantic + structural index** on top of the filesystem, enabling cross-type retrieval in a single query.
4. **Drop everything the new model makes unnecessary**: soft-delete, append-only-notes-archive, sync engine, lifecycle event log, urgency formula, multi-agent claim coordination.

The result is a smaller, more interoperable, more agent-native product whose value proposition is *retrieval over your markdown*, not *task management with a CLI*.

## 3. Scope

### 3.1 In scope for v1

- Generic `Node` + `Edge` primitives backed by markdown files + a derived SQLite index.
- Workspace manifest (`tusk.toml`) declaring node types, edge types, behavior packs, and edge legality.
- Two built-in type packs: `kanban` (ticket + project, with workflow behavior) and `vault` (note + meeting + decision).
- One built-in behavior pack: `workflow` (status finite-state machine).
- CLI (`tusk node`, `tusk edge`, `tusk query`, `tusk reindex`, `tusk doctor`, etc.) and MCP server (stdio + SSE).
- Type-pack ergonomic shortcuts on both surfaces (`tusk ticket open`, `tusk note new`, etc.).
- Filter grammar (TaskWarrior-style, generalized to nodes + edges) — designed as a first-class mini-language with proper lexer / AST / compilation.
- Semantic retrieval via ollama-as-default embedding provider, with API-provider fallbacks (OpenAI, Voyage, Anthropic).
- File watcher for external edits (vim, Obsidian) with index drift reconciliation.
- Respects `.gitignore` and a workspace-level `ignore` list in `tusk.toml` so users can keep non-tusk files in the same git repo without indexing them.
- `tusk doctor` for surfacing validation warnings, off-schema files, dangling edges, empty tag bodies, and embedding-queue retries.
- File-rename pipeline that rewrites all referring edges atomically.

### 3.2 Out of scope for v1

| Item | Reason | Possible future home |
|---|---|---|
| Sync / multi-machine merge engine | Git is the sync | never (architecturally replaced) |
| Migration tool from tusk v0.x | Greenfield is greenfield | v1.x utility if demand |
| Plugin loading for behavior packs | Built-ins only in v1 | v2+ |
| HTTP API | MCP-only for v1 | possible v1.x |
| Bundled local embedding model (ONNX) | Adds binary bloat + CGo complexity | v1.x as alternative provider |
| `crm` type pack (`person`, `company`, `interaction`) | Not headline | v1.x |
| Auto-transition rules (parent auto-completes when children done) | Manifest accepts the option, no-op in v1 | v1.x |
| Recurring nodes / templates / due reminders | Future behavior packs | v1.x+ |
| Web UI / TUI | CLI + MCP only | v2+ |
| Cross-workspace queries | One workspace per query | v2+ |
| Player + claim coordination (multi-agent in one workspace) | Replaced by per-agent isolated workspaces merged via git | never |
| Soft delete | Filesystem + git handle deletion and recovery | never |
| Append-only notes / archive semantics | Filesystem + git handle history | never |
| Lifecycle event log | Single-source local engine doesn't need it | never |
| Urgency formula | Not needed without atomic pop / multi-agent queue | never |
| Levels / taxonomy as a separate system | Subsumed by node types + edge legality | never |
| Annotations as a separate concept | A short note linked to another node *is* an annotation | never |
| Soft tags table | Replaced by tag nodes + `tagged` edges | never |

### 3.3 Migration & repo strategy

- Same repository (`github.com/germanamz/tusk`).
- `v0.14.0` is already tagged as the latest v0.x release; the v0.x line ends there. No further v0 releases.
- Land a final cleanup PR on `main` that removes v0.x-only artifacts that don't carry forward: `docs/status/`, `docs/releases/`, `docs/retrospectives/`, `CHANGELOG.md`, and any roadmap files. This trims the repo to what v1 needs without losing the v0 sources (still reachable at the `v0.14.0` tag).
- Tag the cleanup commit as **`v0-final`** — the canonical "end of v0.x" marker on `main` history.
- Cut a long-lived `v0-archive` branch from `v0.14.0` for any emergency v0 patches (anticipated to be unused).
- Open a `v1` branch from `main` post-cleanup where the rewrite happens. First commit is `git rm` of remaining obsolete source while preserving:
  - `LICENSE`, `.github/`, `.devcontainer/`, `.gitignore`, `Makefile` skeleton, basic `docs/` structure, `CLAUDE.md`, release-please config (rebased for v1).
- Iterate on `v1` until a coherent first cut exists, then merge to `main` and tag `v1.0.0`.
- Update README at the top: "Tusk v1 is a rewrite. The v0.x line ended at `v0.14.0`; see the `v0-archive` branch for v0 sources." Link both.
- No automatic data migration. Optional `tusk import-v0 <path-to-old-db>` utility may follow as a v1.x.

## 4. Architecture

### 4.1 Three-layer model

```
┌──────────────────────────────────────────────────────┐
│ Filesystem (source of truth, in git)                 │
│   tusk.toml, *.md files with frontmatter, binaries   │
└─────────────────────┬────────────────────────────────┘
                      │ read / write
┌─────────────────────▼────────────────────────────────┐
│ Engine (single Go binary)                            │
│   - Manifest loader & validator                      │
│   - Node + Edge writer (transactional)               │
│   - File watcher (fsnotify)                          │
│   - Behavior pack registry                           │
│   - Query compiler & executor                        │
│   - Embedding pipeline                               │
└─────────────────────┬────────────────────────────────┘
                      │ index reads / writes
┌─────────────────────▼────────────────────────────────┐
│ Local index (.tusk/index.db, gitignored)             │
│   nodes, edges, embeddings, last-indexed metadata    │
└──────────────────────────────────────────────────────┘
```

### 4.2 Trust direction

- **Filesystem > index, always.** The index is a cache. On startup, the engine compares index entries against file mtimes and checksums; any drift triggers reindex. The user can `rm -rf .tusk/` and the next `tusk reindex` rebuilds it identically (modulo the embedding cost).
- **External edits are first-class.** Vim, Obsidian, ripgrep, an LLM piping markdown to disk — all of them work without going through the engine. The watcher (when running) keeps the index live; one-shot CLI users invoke `tusk reindex` to bring things current.
- **Off-schema content is warned, not rejected.** A file that violates the manifest is still indexed (with content + embeddings) so it remains queryable. `tusk doctor` surfaces the violation. The engine never deletes, rejects, or silently mutates user content.

### 4.3 Stateless across machines

The binary holds no per-installation state. Anything important is in the files (versioned in git) or the manifest (versioned in git). Cloning a vault to a new machine and running `tusk reindex` produces an identical brain.

## 5. Workspace Layout

### 5.1 Directory structure

```
my-brain/                    # workspace root (git repo)
├── .tusk/                   # gitignored
│   ├── index.db             # derived index (SQLite + sqlite-vec)
│   └── cache/               # embedding cache, OCR cache, etc.
├── .gitignore               # ships ignoring .tusk/
├── tusk.toml                # workspace manifest (committed)
├── notes/
│   ├── auth-rfc.md
│   └── meeting-2026-04-30.md
├── tickets/
│   ├── fix-login-bug.md
│   └── refactor-storage.md
├── tags/
│   └── auth.md
└── attachments/
    └── architecture-diagram.png
```

Folder structure is **user-defined, not schema-defined**. The engine walks the entire workspace looking for `.md` files with a `type:` field in frontmatter. Users can organize by type, project, date, flat, or however they want.

### 5.2 File naming and identity

- **Canonical id = workspace-relative path without extension.** `notes/auth-rfc.md` has id `notes/auth-rfc`. Stored as-is in the index, used in edges, used everywhere internally.
- **No `id:` frontmatter field.** The path *is* the identity. One less reserved key, one less concept.
- **Wikilinks and CLI references use full ids** (workspace-relative paths without extension). A wikilink such as `notes/auth-rfc` wrapped in double square brackets resolves against the canonical id set; identifiers are unambiguous and trivially validated.
- **Renames rewrite all referring edges atomically.** `tusk node move <old> <new>` is an index-driven operation: query all edges where target = old-id, rewrite the source files' frontmatter. The watcher catches external renames and runs the same operation. Rename is therefore a heavy operation in large vaults — acceptable for v1, can be optimized later (batched writes, parallelism).
- **Git renames** (during pull/merge) are *not* auto-handled in v1; doctor surfaces orphaned references and the user runs `tusk reindex --force` (or `tusk node move` if the rename is known) to bring things current.

### 5.3 Ignore patterns

The engine must coexist with non-tusk files in the same workspace (the user's git repo may also hold source code, configs, scripts, etc.). Ignore rules layered top-to-bottom:

1. **`.gitignore`** — anything git ignores is also ignored by tusk. The engine reads `.gitignore` files via standard ignore-pattern semantics, including nested `.gitignore` in subdirectories.
2. **Workspace `[workspace] ignore = [...]` in `tusk.toml`** — explicit additional patterns that aren't in `.gitignore` (e.g., generated files that *are* committed but shouldn't be indexed).
3. **Built-in implicit ignores** — `.tusk/`, `.git/`, anything not matching `*.md` for the structured-node walk (binaries are picked up via sidecar `.md` per §6.5).

A file matching any of the above is skipped during the workspace walk; it does not appear in the index, has no warnings raised against it, and is not affected by reindex. `tusk doctor` reports the count of ignored paths so the user can sanity-check.

## 6. File Format

### 6.1 Frontmatter

Frontmatter is YAML 1.2 (Obsidian-compatible). The manifest is TOML. Manifests benefit from TOML's strictness; frontmatter benefits from YAML's brevity and existing tooling.

```markdown
---
type: ticket                           # required, must match manifest
title: Fix login bug                   # type-declared property
status: active                         # type-declared property (workflow pack)
priority: 3                            # type-declared property
due: 2026-05-15                        # type-declared property
parent: tickets/auth-epic              # edge — manifest declares "parent" as edge type
blocks: [tickets/refactor-storage]     # edge — multi-target
tags: [auth, security]                 # shorthand: materializes "tagged" edges to tags/auth and tags/security
---

# Fix login bug

The bug occurs when users with SSO accounts hit the password reset flow.
See [[auth-rfc]] for context and [[notes/meeting-2026-04-30]] for discussion.
```

### 6.2 Reserved frontmatter keys

Only **`type`** is universally reserved. The set of additional reserved keys depends on which behavior packs are active:

- With `workflow` active on the node's type: the configured `status-property` (default: `status`) is reserved.

Behavior packs declare their reserved keys; the engine refuses (rejects on tusk-owned writes; warns on external) when a user property conflicts.

### 6.3 Frontmatter keys are properties OR edges

A top-level frontmatter key is either a *property* (declared in the node type's `properties:` list) or an *edge* (declared in the manifest's `edge-types`). The same key cannot be both. The engine validates this when loading the manifest.

Any edge type declared with `[edge-types.X]` in `tusk.toml` may appear as a top-level frontmatter key on any node of an allowed `from` type, with a value that is either a scalar string (single target id) or a list of strings (multiple target ids). This is the canonical mechanism for declaring non-`ref` edges in frontmatter. The 2026-05-18 edges-from-frontmatter design made this the only durable path; `tusk edge add` / `tusk_edge_add` MCP now mutate frontmatter directly and the index is rebuilt from it.

`tags: [...]` is a special-case shorthand declared by type packs: each entry is a tag *name* (e.g., `auth`) which is resolved to the tag node at the path declared by the active tag pack — by default `tags/<name>` (so `tags: [auth]` materializes a `tagged` edge to `tags/auth`). The pack's tag-path-template is configurable in the manifest.

When a `tags:` reference resolves to a path with no corresponding node, the engine auto-creates the tag node with an empty body. Empty tag bodies surface in `tusk doctor` as "consider adding context here" — tag bodies become part of the embedded payload and improve semantic retrieval. Auto-creation is gated by a manifest flag for workspaces that prefer strict resolution.

### 6.4 Wikilinks

Body wikilinks wrap a workspace-relative path (without extension) in double square brackets — the target is a canonical node id. At index time, each resolved wikilink materializes an implicit `references` edge from the containing node to the target.

A wikilink whose target has no corresponding node surfaces in `tusk doctor` as a dangling reference; the wikilink still renders as text in the body but no edge is materialized.

Typed edges live in frontmatter; wikilinks are navigational shorthand.

### 6.5 Binary nodes

A binary file (image, PDF, audio) gets a sidecar `.md` if metadata or extracted text is wanted:

```markdown
---
type: image
binary: ../attachments/architecture-diagram.png
title: Auth flow diagram
extracted-text: |
  [auto-populated from OCR on index]
---
```

For v1, binary-without-sidecar is allowed but not first-class — the index records filename + size only, no extracted text or embedding. OCR/transcript extraction may land in v1.x.

## 7. Workspace Manifest

The manifest is `tusk.toml` at the workspace root. It declares the contract between the user and the engine.

### 7.1 Three composition layers

```toml
[workspace]
name = "my-brain"
type-packs = ["kanban", "vault"]   # ergonomic: list of activated packs

# Layer 1: type-pack overrides (optional)
[type-packs.kanban.workflow]
states = [
  { name = "backlog",  initial = true },
  { name = "doing",    start = true },
  { name = "review",   terminal = false },
  { name = "shipped",  terminal = true, done = true },
]
transitions = [
  { from = "backlog", to = "doing" },
  { from = "doing",   to = "review" },
  { from = "review",  to = "shipped" },
  { from = "review",  to = "doing" },
]

# Layer 2: inline custom node types
[node-types.decision]
description = "A captured decision with rationale and date"
properties = [
  { name = "title",       type = "string", required = true },
  { name = "decided-at",  type = "date",   required = true },
  { name = "status",      type = "enum",   values = ["proposed", "accepted", "rejected", "superseded"] },
  { name = "rationale",   type = "markdown" },
]

# Layer 3: inline custom edge types
[edge-types.supersedes]
description = "This node supersedes another"
from        = ["decision"]
to          = ["decision"]
cardinality = "many-to-one"
ordered     = false
inverse     = "superseded-by"
```

### 7.2 Type packs (built-in, ship with the binary)

Each pack is a coherent bundle of node types + edge types + behavior activations. v1 ships:

- **`kanban`** — `ticket`, `project` node types; `parent`, `blocks`, `tagged` edges; `workflow` behavior on tickets.
- **`vault`** — `note`, `meeting`, `decision` node types; `references`, `relates-to` edges; no behaviors.
- **`tags`** (auto-included by any pack that uses tags) — universal `tag` node type and `tagged` edge.

A user can disable a pack's ergonomic shortcuts (`[type-packs.kanban] shortcuts = false`) for users who want only the generic CLI surface.

### 7.3 Property types

A small fixed set:

| Type | Notes |
|---|---|
| `string` | scalar |
| `int`, `float` | scalar |
| `bool` | scalar |
| `date`, `datetime` | ISO 8601 |
| `enum` | with `values: [...]` |
| `markdown` | rendered field; indexed for search |
| `ref` | edge-shorthand alias — `{ type = "ref", to = "person" }` is sugar for an edge type |
| `list-of(T)` | array of any of the above |

**No nested objects, no arbitrary JSON blobs.** Anything more complex becomes a separate node connected by an edge. This is the discipline that keeps the index sane and the graph navigable.

### 7.4 Edge type declarations

Every edge type declares:

- `from` / `to` — allowed source and target node types. `["*"]` means any.
- `cardinality` — one of `one-to-one`, `one-to-many`, `many-to-one`, `many-to-many`.
- `ordered` — boolean. If true, the edge list preserves order (e.g., children of a parent for sibling-ordering).
- `inverse` — name of the derived inverse edge for query convenience. If absent, queries can still traverse backward via a `<-` operator.
- `acyclic` — boolean. If true, the engine validates no cycle is created on each edge add.

### 7.5 Validation behavior

| Source of write | Behavior on violation |
|---|---|
| Tusk-owned writes (CLI, MCP) | Reject the write before touching the file |
| External edits (watcher, reindex) | Index the node; surface violations via `tusk doctor` |
| Malformed `tusk.toml` (TOML invalid or schema-invalid) | Engine refuses to start; CLI exits with explanatory error |
| Manifest changes that break existing nodes | Engine starts; `tusk doctor` surfaces every newly-violating node |

Tusk-owned writes are gated to enforce correctness. External writes are accepted; the engine's role is to surface problems, not destroy data.

## 8. Behavior Packs

A behavior pack is engine-level logic that activates on node types via the manifest.

### 8.1 v1 ships one behavior pack: `workflow`

```toml
[behaviors.workflow]
applies-to = ["ticket"]
status-property = "status"

states = [
  { name = "pending",   initial = true },
  { name = "active",    start = true },
  { name = "completed", terminal = true, done = true },
]

transitions = [
  { from = "pending",   to = "active" },
  { from = "active",    to = "completed" },
  { from = "active",    to = "pending" },
  { from = "completed", to = "pending" },
]

# Optional auto-transitions (no-op in v1, manifest accepts them; activated in v1.x)
auto-complete-parent = false
auto-revert-parent   = false
```

**Roles:** `initial`, `start`, `terminal`, `done`. Deletion is a filesystem operation; there is no `delete` role. `highlight` and `dim` are presentation hints.

**Enforcement:**
- The workflow pack hooks into `tusk node modify` (and its MCP equivalent). When a write changes the configured `status-property`, the workflow validator checks that `(old-status, new-status)` is a legal transition; invalid transitions are rejected before the file is touched.
- External edits that change the status frontmatter are validated at index time; off-schema status surfaces in `tusk doctor` as a warning. The node is still indexed.
- There is no dedicated `tusk transition` verb. Type packs (e.g., `kanban`) ship ergonomic shortcuts like `tusk ticket start` and `tusk ticket done` that resolve to `tusk node modify <id> --prop status=<state>`; ergonomics are pack territory, not engine territory.

### 8.2 Future behavior packs (architectural placeholders)

The behavior-pack interface is internal Go in v1; no plugin loading. The following names exist as placeholders so the architecture admits them without retrofit:

- `due-reminders` — local notifications when due dates approach
- `recurring` — auto-creating instances of a template node on a schedule
- `vector-watcher` — re-embed when content drifts substantially

User-defined plugin packs land in v2+.

## 9. Indexing

The index lives at `.tusk/index.db` (SQLite, gitignored). Three update modes:

### 9.1 Transactional (engine-owned writes)

When the user invokes a tusk command (CLI or MCP):

1. Acquire advisory lock on the file (using temp-file-rename pattern for atomicity).
2. Validate against manifest.
3. Write the markdown file atomically (write temp, fsync, rename).
4. Update the index (node row, edge rows, embedding job marker).
5. Release lock.

If any step fails, both the file and the index roll back (file via temp-file-discard, index via SQLite transaction rollback). Default path for agent activity.

### 9.2 File watcher (long-running processes)

`tusk mcp` (the long-running MCP server) starts a `fsnotify`-backed watcher on the workspace.

1. Watcher debounces (~500ms) to coalesce rapid saves.
2. Engine reads the file, parses frontmatter, validates against manifest.
3. Updates the node's index row and edges.
4. Schedules an async embedding refresh (see §10).
5. If validation fails: keeps the prior valid index entry, surfaces a warning via `tusk doctor`. The file is not rejected.

Watcher is opt-in via `[workspace] watch = true` (default `true` when `tusk mcp` is running, `false` for one-shot CLI commands).

### 9.3 Explicit reindex

`tusk reindex [path] [--force] [--no-embed]` walks the workspace (or a subpath), compares each file's checksum against `index.last_checksum`, and reprocesses any that drifted. Used for:

- First run after `tusk init` or after `git pull`
- Recovery after `.tusk/` was deleted or corrupted
- Forced rebuild via `--force`
- Embedding pass via the included default; `--no-embed` skips the embedding phase

There is *no* separate `tusk embed` command — embedding is part of reindex.

**Performance target:** 10k nodes reindex in under 5 seconds on commodity hardware (excluding embeddings). Embeddings are async — see §10. Reindex is single-threaded; parallel parsing is a future addition for very large vaults.

### 9.4 Drift detection

For each file the index stores: `path`, `last_mtime`, `last_size`, `last_checksum` (sha256). Reindex skips files whose mtime+size match. Mtime collisions (same mtime+size, different content) are guarded by checksum.

For embeddings the index also stores `embedding_content_hash` — a hash over the indexed-content slice (frontmatter properties + body, normalized). Embedding refresh skips when content hash matches even if file mtime changed.

### 9.5 Index schema (sketch — final shape during implementation planning)

```
nodes              (id, type, path, title, properties_json, last_mtime, last_size, last_checksum)
edges              (id, type, source_id, target_id, ordinal, source_path)
embeddings         (node_id, chunk_idx, model, content_hash, vector, dim)
embed_queue        (node_id, enqueued_at, attempts, last_error)
manifest_snapshot  (json, snapshot of the manifest at last load)
warnings           (node_id, kind, message, since)
```

`source_path` on edges captures *which file declared the edge* — needed to update edges when their source file changes or moves.

### 9.6 Bootstrap

```
$ tusk init [--type-pack kanban,vault]
```

Creates `tusk.toml` with chosen packs, creates `.tusk/`, appends to `.gitignore`, creates an empty `index.db` with schema. No nodes yet. User can then `tusk node create` or drop in markdown files and `tusk reindex`.

### 9.7 Manifest changes

When `tusk.toml` changes, the next engine invocation detects the diff against `manifest_snapshot`:

- New types/edges → no-op for existing nodes.
- Removed types/edges → existing nodes flagged as off-schema warnings.
- Modified property/edge constraints → re-validate every affected node, surface violations.

The engine never silently mutates files due to a manifest change. Migrations are user-driven.

### 9.8 Concurrency

- SQLite WAL mode, `busy_timeout=5000`.
- Cross-process write coordination uses an advisory lockfile in `.tusk/`. The lock is per-write, not whole-process: a long-running `tusk mcp` and a one-shot `tusk` CLI invocation coexist; reads and queries from either side proceed concurrently.
- Single-writer; multiple readers. Writes serialize on the per-workspace lock.
- Index file lives in `.tusk/`, so cross-machine concurrency is not a concern (each machine has its own index).

### 9.9 Rename rewrite pipeline

When a node is renamed (via `tusk node move`, or detected by the watcher):

1. Acquire workspace-wide write lock.
2. Query the index for all edges where `target_id = old-id` OR `source_id = old-id`.
3. For each affected source file, parse the frontmatter, rewrite the relevant edge values, write the file atomically.
4. Update the index in the same transaction (rename the node row, update edge target/source ids, update path).
5. Release lock.

The cost is O(referrers). For a popular node with many incoming edges, this can touch many files. Acceptable for v1 — a vault with 10k nodes and a hub node with 100 referrers rewrites in <1 second on commodity hardware.

## 10. Retrieval

Three query shapes: structural, semantic, hybrid.

### 10.1 Structural query

A TaskWarrior-flavored filter grammar generalized to nodes + edges.

```
type=ticket status=active priority>=3 +auth blocks->
type=note created>=2026-04-01 has-edge:relates-to
type=ticket parent->type=project parent->name="auth"
```

- `key=value` → property equality
- `key>value`, `<`, `>=`, `<=`, `!=` → comparators
- `key=a..b` → ranges
- `+tag`, `-tag` → tag presence/absence (sugar for `tagged-> tag-name`)
- `<edge-name>->` → has any outgoing edge of that type
- `<edge-name>->key=value` → outgoing edge target matches predicate (1-hop)
- `<edge-name><-` → has any incoming edge of that type (e.g., `blocks<-` finds nodes blocked by something)
- `<edge-name><-key=value` → incoming edge source matches predicate (1-hop)
- `tree=<id>`, `parent=<id>`, `root=<id>` → graph-traversal shortcuts
- AND/OR/NOT/parens

Resolves entirely against the index; no embedding cost.

**Designed as a first-class mini-language.** v1 includes a proper lexer / AST / compilation pipeline for the filter grammar. The grammar deserves its own design subsection during implementation planning — this spec records the surface, not the parser internals. The compilation target is parameterized SQL against the index.

### 10.2 Semantic query

```
tusk query --semantic "auth bug in password reset flow" --top 10
tusk query --semantic "decisions about storage backend" --type decision --top 5
```

The query string is embedded with the active model and runs nearest-neighbor over the `embeddings` table. Returns ranked list with similarity scores. Optional structural filters narrow the candidate pool *before* the NN search.

### 10.3 Hybrid (recommended default for agents)

```
tusk query 'type=ticket status=active' --semantic "auth flow" --top 10
```

Structural filter gives a candidate set; semantic ranking sorts within it.

### 10.4 What gets embedded

For each node, the engine builds an embedding payload:

```
[type] {type}
[title] {title-property}
{frontmatter-properties relevant to retrieval, normalized}
---
{body markdown, stripped of formatting noise}
```

Embedded with the active model, hashed (`embedding_content_hash`), stored. Refresh when payload hash changes. v1 ships a single chunking strategy (see §10.7); the engine exposes a `ChunkingStrategy` interface so future strategies can be added without touching the core embedding pipeline.

**Edges are not embedded.** They're queried structurally.

### 10.5 Embedding provider

**Default: ollama at `localhost:11434`** with `nomic-embed-text` as the default model. If ollama is not reachable, tusk emits a clear setup hint pointing to install instructions or alternative providers.

API providers (OpenAI, Voyage, Anthropic) are first-class fallbacks declared in the manifest:

```toml
[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768

# Or:
[embeddings]
provider = "openai"
model    = "text-embedding-3-small"
api-key  = "$OPENAI_API_KEY"   # env var lookup
dim      = 1536
```

Switching providers triggers a full re-embed (different vector spaces). The engine warns and asks for confirmation.

Bundled local embeddings are **out of v1**. When this lands as a v1.x alternative provider, the design target is "ONNX runtime + a community model" — the standard is ONNX (so a user can swap in any community-published ONNX embedding model), not a single hardcoded model. The runtime choice is what gets bundled; specific models are configurable per-workspace.

### 10.6 Async embedding pipeline

Writes don't block on embedding. On node write/update, the engine enqueues the node id in the `embed_queue` table. A background worker (running inside `tusk mcp`, or invoked once by `tusk reindex`) drains the queue and populates the embeddings table. Until a node is embedded, semantic queries skip it; structural queries return it normally.

When the embedding provider is unreachable, affected nodes remain in `embed_queue` with their `last_error` recorded; `tusk doctor` surfaces the queue depth and last error. The user retries via `tusk reindex` or by restarting the provider. A half-populated index degrades gracefully — structural queries always work; semantic queries just skip nodes whose embeddings are pending.

### 10.7 Chunking strategy

The default chunking strategy is **`whole-document`**: the engine embeds the full payload as a single vector. When the payload exceeds the active model's max-token window, the engine truncates at the boundary and `tusk doctor` surfaces the truncation so the user can split the node manually.

Chunking is pluggable via a `ChunkingStrategy` interface (`Chunk(payload) → []Chunk`). The `embeddings` table carries `chunk_idx` so multi-chunk strategies fit the schema without modification. Strategies are selected per-workspace via `[embeddings] chunking = "..."`.

### 10.8 Result shape

```json
{
  "results": [
    {
      "id": "tickets/fix-login-bug",
      "type": "ticket",
      "title": "Fix login bug",
      "path": "tickets/fix-login-bug.md",
      "score": 0.84,
      "snippet": "...users with SSO accounts hit the password reset...",
      "edges": ["parent:tickets/auth-epic", "tagged:tags/auth", "tagged:tags/security"]
    }
  ],
  "elapsed-ms": 23,
  "total-candidates": 142,
  "structural-filtered": 38,
  "model": "nomic-embed-text"
}
```

JSON via `--json` flag; readable text otherwise.

## 11. Surfaces

Two surfaces share one engine. Every CLI verb has a 1:1 MCP tool. Type packs add ergonomic shortcuts on top.

### 11.1 Generic CLI

```bash
# Workspace
tusk init [--type-pack kanban,vault]
tusk reindex [path] [--force] [--no-embed]
tusk doctor                              # surface validation warnings, off-schema files, dangling edges/wikilinks, empty tag bodies, embed-queue retries
tusk status                              # quick summary: node counts by type, last reindex, queue depth

# Node lifecycle
tusk node create --type <t> [--title "..."] [--prop key=value] [--from-stdin]
tusk node get <id-or-path>
tusk node list [filter-expr]
tusk node modify <id> [--prop key=value] [--unset key]
tusk node move <id> <new-path>           # atomic rename + edge rewrite (see §9.9)
tusk node delete <id>                    # rm the file + remove the node and its outgoing edges from the index; incoming edges become dangling and surface in doctor

# Edges
tusk edge add <type> <source-id> <target-id>
tusk edge remove <type> <source-id> <target-id>
tusk edge list [--from <id>] [--to <id>] [--type <t>]

# Query / retrieval
tusk query [filter-expr] [--semantic "text"] [--top N] [--sort <spec>] [--json]
```

Status changes go through `tusk node modify --prop status=<state>`; the workflow pack validates the transition during the modify. "Next best matching node" is expressed as `tusk query <filter> --sort <spec> --top 1`. Type packs may bundle ergonomic shortcuts on top of these (see §11.2).

The default sort for `tusk query` is configured per workspace via `[workspace] default-sort = "..."` and overridable per call via `--sort priority-desc,due-asc,modified-desc`.

### 11.2 Type-pack ergonomic shortcuts

Type packs declare ergonomic verbs that resolve to generic ones. Workflow ergonomics (start/done/next) live in packs; the engine validates transitions during `node modify`, packs provide the verbs.

```bash
# Shipped by the kanban pack
tusk ticket open "title"        # → node create --type ticket --title "..."
tusk ticket start <id>          # → node modify <id> --prop status=<start-state>
tusk ticket done <id>           # → node modify <id> --prop status=<done-state>
tusk ticket list [filter]       # → node list type=ticket [filter]
tusk ticket next [filter]       # → query type=ticket [filter] --sort <pack-default> --top 1

# Shipped by the vault pack
tusk note new "title"           # → node create --type note --title "..."
tusk note list [filter]         # → node list type=note [filter]
```

The pack declares the start/done states (resolved from the workflow's `start` and `done` roles) and the default sort for `next`. Shortcuts can be disabled per-pack in the manifest.

### 11.3 MCP surface

Every CLI verb maps to a tool. Naming convention: `tusk_<noun>_<verb>`.

```
tusk_init
tusk_reindex
tusk_doctor
tusk_status

tusk_node_create
tusk_node_get
tusk_node_list
tusk_node_modify
tusk_node_move
tusk_node_delete

tusk_edge_add
tusk_edge_remove
tusk_edge_list

tusk_query
```

Type-pack-aware wrappers (`tusk_ticket_open`, `tusk_note_new`) ship as additional tools when the pack is active. They're discovery-friendly for agents — an MCP-connected LLM listing tools sees both the generic and the pack-specific shapes.

**MCP transport:** stdio + SSE, same as v0.x. No HTTP API in v1.

### 11.4 Output modes

- Default: human-readable text, color-aware, terminal-width-aware.
- `--json`: structured JSON for scripting / agents.
- `--quiet`: machine-friendly minimal (e.g., just IDs, one per line).

Same set across every command.

## 12. Storage Choice

SQLite + `sqlite-vec` (or `sqlite-vss` if benchmarks favor it) for the local index. Rationale:

- The workload is mostly structured queries with bounded graph traversals (1-3 hops); recursive CTEs in SQLite handle this comfortably at the scales target users will see.
- Vector index is well-served by `sqlite-vec` (production-ready, simple API, no separate process).
- Single-file index → trivial to delete and rebuild.
- Existing tusk team has SQLite expertise; no reason to add a new stack.

An embedded graph DB (KuzuDB or similar) becomes a candidate if the workload shifts to multi-hop graph analytics. v1's retrieval pattern stays in SQLite's comfort zone.

## 13. Open Questions

These deserve their own design pass during implementation planning.

1. **Filter grammar precise specification.** The grammar admits multi-hop edge traversal (e.g., `parent->parent->name="foo"`); evaluation guards (max hop depth, max candidate set) bound performance. Lexer / AST / compilation are designed separately, with parameterized SQL + recursive CTEs as the compilation target.

2. **Behavior-pack hook surface.** Behaviors compose over a small base set of hooks on Node and Edge writes (`OnNodeRead`, `OnNodeWrite`, `OnEdgeAdd`, `OnEdgeRemove`). The `workflow` pack's transition validation is a particular configuration of these hooks. Concrete interface shapes are designed during implementation planning so user-defined behaviors (v2+) can compose through the same primitives.

## 14. Design discipline (notes for future contributors)

- **The filesystem is the source of truth, always.** If a design choice would make tusk feel like a database, push back.
- **Off-schema content is warned, not rejected.** Tusk does not destroy or refuse user data because it can't validate it.
- **Behaviors are opt-in.** A workspace with no type packs and no behavior packs is a valid tusk workspace — it's a markdown vault with retrieval.
- **Node + edge are the only modeling primitives.** Anything else is config or behavior. Resist the urge to add new entity types at the engine level.
- **Single-actor per workspace.** Multi-agent coordination is a workspace-multiplicity problem, not a tusk problem.

## 15. References

- `v0.14.0` — last v0 release tag; preserved.
- `v0-final` — cleanup commit marking the end of v0.x on `main`; v1 work begins after this tag.
- `v0-archive` — long-lived branch from `v0.14.0` for emergency v0 patches (anticipated to be unused).
- `PRODUCT.md` — v0.x product description; will be substantially rewritten for v1.
- v0.x status reports, retrospectives, and release notes — reachable via the `v0.14.0` tag and the `v0-archive` branch; removed from `main` as part of the `v0-final` cleanup.
- v0.15 design notes (Workspace Mode + Sync) — preserved in tusk task notes; superseded by this spec.
- Obsidian — primary inspiration for the filesystem-as-source-of-truth model.
- TaskWarrior — inspiration for the filter grammar and CLI ergonomics.
- MCP (Model Context Protocol) — agent surface.
- `sqlite-vec` — vector index implementation.
- Ollama — default local embedding provider.
