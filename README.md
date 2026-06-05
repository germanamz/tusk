# Tusk

**A local-first agent brain.** Tusk turns a directory of markdown files into a
schema-validated, semantically-indexed graph — queryable from the CLI and from
any MCP-compatible agent (Claude Code, Cursor, etc.).

Files are the source of truth. Git is the history. Tusk is the indexer and the
retrieval engine.

```
markdown vault  ──▶  tusk indexer  ──▶  SQLite graph + embeddings
                                              │
                                              ├─▶ CLI       (tusk query, tusk node …)
                                              └─▶ MCP tools (tusk_query, tusk_node_create, …)
```

- **Local first.** No service to log in to. The index lives in `.tusk/` next to your files.
- **Schema-validated.** Node and edge types are declared in `tusk.toml`. Off-schema content is warned, never rejected.
- **Structural + semantic.** A compact filter grammar for the graph (`key=value` / `key:value`, ranges, edge traversal, boolean composition), Ollama-backed embeddings for similarity, and a hybrid mode that filters then ranks.
- **External edits are first-class.** Vim, Obsidian, an LLM piping markdown — they all work; the watcher keeps the index live.
- **One engine, two surfaces.** Every CLI verb has a 1:1 MCP tool.

---

## Installation

### Prerequisites

- (Optional, for semantic search) [Ollama](https://ollama.com) running locally with an embedding model, e.g. `ollama pull nomic-embed-text`.

### One-liner (prebuilt binary)

```bash
curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | sh
```

Detects your OS/arch, downloads the latest GitHub release, drops the `tusk` binary into `~/.local/bin` (override with `INSTALL_DIR=/usr/local/bin`), and installs its man pages into `~/.local/share/man` (override with `MAN_DIR`; `man tusk` works once that dir is on your `MANPATH`). Pin a specific release with `TUSK_VERSION=v1.1.0`. Prebuilt archives ship for darwin/linux/windows on amd64 + arm64.

### From source

Requires Go 1.26+.

```bash
git clone https://github.com/germanamz/tusk
cd tusk
make build
# binary at ./bin/tusk — move it onto your PATH
install bin/tusk /usr/local/bin/tusk
```

Or, without cloning:

```bash
go install github.com/germanamz/tusk/cmd/tusk@latest
```

Verify:

```bash
tusk --version
```

### Updating

Tusk has no self-update command — re-run whichever install method you used and the binary gets overwritten in place. Your workspace and `.tusk/` index are untouched.

```bash
# Prebuilt binary — same one-liner; defaults to the latest release
curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | sh

# Pin a specific version
curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | TUSK_VERSION=v1.2.0 sh

# From source
cd tusk && git pull && make build && install bin/tusk /usr/local/bin/tusk

# go install
go install github.com/germanamz/tusk/cmd/tusk@latest
```

After a major upgrade, run `tusk reindex` to pick up any indexer changes, then `tusk doctor` to confirm the workspace is healthy.

---

## Quickstart

> For per-command reference (flags, examples), see
> [docs/cli/](docs/cli/README.md). For multi-command recipes, see
> [docs/cli/workflows.md](docs/cli/workflows.md). Man pages are in [`man/`](man/) — `man -M man tusk` after cloning.

```bash
# 1. Initialize a workspace in the current directory
mkdir my-brain && cd my-brain
tusk init --name my-brain

# 2. Add a built-in type pack so you have some node types
tusk pack add vault     # note, meeting, decision + references/relates-to edges
tusk pack add tags      # tag node type + tagged edge
tusk pack add kanban    # ticket node type + workflow + parent/blocks edges

# 3. Create a node
tusk node create --path notes/hello.md --type note --title "Hello, Tusk"

# 4. Build the index (also runs after every CLI write)
tusk reindex

# 5. Query the graph
tusk node list 'type=note'
tusk query 'type=ticket status=active' --sort '+priority,-due'

# 6. Get a quick health check
tusk status
tusk doctor
```

---

## How information is structured

A Tusk workspace is just a directory:

```
my-brain/
├── tusk.toml                # workspace manifest (committed)
├── .tusk/                   # gitignored — local SQLite index
│   └── tusk.db
├── .gitignore
├── notes/
│   └── auth-rfc.md
├── tickets/
│   └── fix-login-bug.md
└── tags/
    └── auth.md
```

### Nodes are markdown files

Every `.md` file with a `type:` field in YAML frontmatter is a **node**. The file path (minus the extension) is the canonical **node id** — no separate id field.

```markdown
---
type: ticket
title: Fix login bug
status: active
priority: high
due: 2026-05-15
parent: tickets/auth-epic
blocks: [tickets/refactor-storage]
tags: [auth, security]
---

# Fix login bug

The bug occurs when users with SSO accounts hit the password reset flow.
See [[notes/auth-rfc]] for context.
```

- `type` is the only universally reserved key.
- Other frontmatter keys are either **properties** (string / int / date / enum / ref / list-of) or **edges** (declared in `tusk.toml`).
- `[[notes/auth-rfc]]` body wikilinks materialize as edges to that node id for any edge type declared with `wikilinks = true` (e.g. the `vault` pack's `references` edge).

### Edges connect nodes

Edges are typed, declared in the manifest, and can be created two ways:

- **Frontmatter** — the natural place. `parent: tickets/auth-epic` declares a `parent` edge.
- **CLI / MCP** — `tusk edge add --type blocks --source tickets/a --target tickets/b`.

Edge declarations enforce legality (`from`/`to` types), cardinality, ordering, and optional `acyclic = true` (cycles are rejected at write time).

### The manifest defines the schema

`tusk.toml` is the contract between you and the engine. A minimal manifest:

```toml
[workspace]
name = "my-brain"
ignore = ["bin/", "node_modules/", "*.test"]

[embeddings]
provider = "ollama"
endpoint = "http://localhost:11434"
model    = "nomic-embed-text"
dim      = 768

[node-types.note]
description = "A free-form markdown note"
properties = []

[node-types.decision]
description = "A captured decision"
properties = [
    { name = "decided-at",  type = "date", required = true },
    { name = "status",      type = "enum", values = ["proposed", "accepted", "rejected", "superseded"] },
    { name = "supersedes",  type = "ref",  to = "decision" },
]

[edge-types.references]
description = "Implicit edge materialized from body wikilinks"
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
inverse     = "referenced-by"
```

`ref` properties auto-materialize edge types of the same name — declaring `supersedes` as a `ref` to `decision` gives you a `supersedes` edge for free.

### Type packs (built-in templates)

Instead of declaring everything by hand, `tusk pack add <name>` splices a curated TOML block into your manifest:

| Pack | Adds |
|------|------|
| `vault` | `note`, `meeting`, `decision`; `references` (wikilinks) + `relates-to` |
| `tags` | `tag` node + `tagged` edge (with `tags: [a, b]` frontmatter shorthand) |
| `kanban` | `ticket` node with workflow-validated `status`; `parent` (WBS) + `blocks` edges |
| `dev` | `spec`, `plan`, `handoff`, `package` — dogfooding pack for tracking software projects |

Packs compose: add `vault` + `tags` + `kanban` and you have notes, decisions, tags, and a kanban workflow on top.

```bash
tusk pack add vault
tusk pack add tags
tusk pack add kanban
```

You can also load a pack from a URL or local file:

```bash
tusk pack add https://example.com/packs/research.toml
tusk pack add file://$PWD/my-pack.toml
```

---

## Indexing

The index lives in `.tusk/tusk.db` (SQLite + WAL). It is **derived state** — delete `.tusk/` and `tusk reindex` rebuilds it identically.

### One-shot reindex

```bash
tusk reindex
# Reindex done: 142 indexed, 0 removed, 3 skipped
```

`reindex` walks the workspace, parses every `.md`, validates frontmatter against the manifest, resolves refs + wikilinks into edges, and enqueues embeddings.

### Live watcher

```bash
tusk watch
```

Runs fsnotify against the workspace and applies edits incrementally. Drains the embed queue in the background.

### What gets indexed

- Every `.md` file with a `type:` field, anywhere in the workspace.
- Filtered through `.gitignore` + `[workspace] ignore` patterns.
- `.tusk/` and `.git/` are always ignored.

Off-schema content is **warned, not rejected** — a file with an unknown `type:` or a property violation still gets indexed (so it stays queryable) and surfaces in `tusk doctor`.

---

## Querying

### Structural filter

A compact filter grammar that compiles to parameterized SQL. Property
comparisons accept `=` or `:` interchangeably; the rest of the grammar uses
the operators below.

```bash
# Property predicates (`=` and `:` are equivalent)
tusk query 'type=ticket status=active priority=high'
tusk query 'type:note created>=2026-04-01'

# Edge traversal: -> outgoing, <- incoming
tusk query 'type=ticket blocks->type=ticket'        # tickets that block other tickets
tusk query 'type=note <-references type=spec'      # notes referenced by specs

# Multi-hop
tusk query 'type=ticket parent->parent->title="auth-epic"'

# Sort + pagination
tusk query 'type=ticket status=active' --sort '+priority,-due' --take 10
```

### Semantic search

Requires `[embeddings]` configured (Ollama by default). Embedding runs asynchronously after writes; until a node is embedded, it's invisible to semantic queries (and surfaces in `tusk doctor`).

```bash
tusk query 'type=*' --semantic "auth bug in password reset flow" --take 5
```

### Hybrid (recommended for agents)

Structural filter narrows the candidate set; semantic similarity ranks within it.

```bash
tusk query 'type=ticket status=active' --semantic "login flow" --take 10
```

### JSON output

Add `--json` to any query for structured output that's easy to pipe into an LLM or a script.

```bash
tusk query 'type=decision' --semantic "storage backend" --top 3 --json
```

---

## MCP server (Claude Code, Cursor, …)

`tusk mcp` runs an MCP server that exposes every CLI verb as a tool. Stdio is the default transport; SSE is available on a port.

```bash
tusk mcp                      # stdio (for Claude Code / Cursor / Codex)
tusk mcp --transport sse --addr :8765
```

It holds the workspace open for the lifetime of the session: a single SQLite handle, an embed-queue drainer, and an fsnotify watcher all live in the same process so the index stays warm across tool calls.

### Wiring it into Claude Code

```bash
claude mcp add tusk -- /usr/local/bin/tusk mcp
```

Or directly in `~/.claude.json`:

```json
{
  "mcpServers": {
    "tusk": {
      "command": "/usr/local/bin/tusk",
      "args": ["mcp"],
      "cwd": "/path/to/my-brain"
    }
  }
}
```

### Available MCP tools

| Tool | What it does |
|------|--------------|
| `tusk_status` | node counts by type, edge count, queue depth, last reindex |
| `tusk_doctor` | validation warnings, dangling refs, embed-queue retries |
| `tusk_node_get` / `tusk_node_list` | read by id or filter |
| `tusk_node_create` / `tusk_node_modify` / `tusk_node_move` / `tusk_node_delete` | write |
| `tusk_edge_add` / `tusk_edge_remove` / `tusk_edge_list` | edge CRUD |
| `tusk_query` | structural + optional `semantic` ranking |
| `tusk_reindex` | force a full walk |

Workspace-config commands (`tusk pack add`) stay CLI-only in v1.

---

## Health and diagnostics

```bash
tusk status     # node counts, edge count, embed-queue depth, last reindex timestamp
tusk doctor     # validation warnings, dangling refs/wikilinks, embed-queue errors, embed stats
```

`doctor` is the place to look when:

- semantic queries seem to be missing nodes → check the embed-queue depth and last error
- a wikilink points to nothing → dangling-ref warning surfaces it
- a manifest change just landed → re-validate every affected node

---

## Architecture

Single Go binary, single SQLite index, single embedding provider (Ollama for now).

```
┌──────────────────────────────────────────────────────┐
│ Workspace (markdown + tusk.toml)                     │
└─────────────────────┬────────────────────────────────┘
                      │ fs walk / fsnotify
┌─────────────────────▼────────────────────────────────┐
│ Engine (cmd/tusk + internal/*)                       │
│   manifest · node · edge · reindex · filter · embed  │
│   watcher · behaviors · mcp                          │
└─────────────────────┬────────────────────────────────┘
                      │ reads / writes
┌─────────────────────▼────────────────────────────────┐
│ .tusk/tusk.db   (SQLite WAL, embeddings table)       │
└──────────────────────────────────────────────────────┘
```

- **Filesystem > index, always.** The index is a cache; `rm -rf .tusk/ && tusk reindex` rebuilds it.
- **Stateless across machines.** Clone the vault, reindex, get an identical brain.
- **Single-writer, many-readers.** SQLite WAL + a workspace-wide advisory lock so `tusk mcp` and one-shot CLI calls coexist.

Product vision and design principles live in [`PRODUCT.md`](PRODUCT.md). Per-package notes live in [`docs/packages/`](docs/packages/).

---

## Development

```bash
make build        # ./bin/tusk
make test         # unit tests
make test-race    # with race detector
make vet
make lint         # golangci-lint
make fmt
```

See [`STYLE.md`](STYLE.md) for the codebase conventions and [`CONTRIBUTING.md`](CONTRIBUTING.md) for how to propose changes.

## v0 references

- [`v0.14.0`](https://github.com/germanamz/tusk/releases/tag/v0.14.0) — last v0 release tag; preserves all v0 sources. Branch from it if a v0 patch is ever needed.

## License

[Apache 2.0](LICENSE)
