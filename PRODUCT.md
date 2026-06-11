# Tusk — Product

**Tusk is a local-first agent brain: a markdown vault with a smart,
schema-validated, semantically-indexed graph layered on top.**

Files (markdown + a `tusk.toml` manifest) are the source of truth. Git is the
history and the sync mechanism. Tusk is an indexer, schema enforcer, and
retrieval engine that runs locally as a single binary, exposes a CLI and an MCP
server, and never owns the data it indexes.

The headline capability: an agent (or human) issues one query and gets back the
most semantically- and structurally-relevant nodes regardless of type, drawn
from a vault that any other tool — vim, Obsidian, ripgrep, git — can also read.

> **Status:** v1 has shipped. For install and usage see the [`README`](README.md)
> and [`docs/cli/`](docs/cli/README.md); per-package internals live in
> [`docs/packages/`](docs/packages/); forward-looking work is tracked in
> [`docs/roadmap.md`](docs/roadmap.md).

## Why Tusk exists

Task-manager models fragment retrieval across multiple sources of truth: tasks,
notes, annotations, tags, and people each live in their own table with their own
shape. An agent that needs context for a piece of work has to know *which*
surface to query for *which* kind of information, and orchestrate across them —
long research iterations, brittle prompts.

Tusk's fix is structural, not additive:

1. **Collapse every entity into a generic node + edge substrate**, with a
   workspace manifest declaring the types, properties, and edge legality.
2. **Move from database-as-source-of-truth to filesystem-as-source-of-truth**,
   so content lives as markdown that any tool can read and git can version.
3. **Layer a unified semantic + structural index** on top of the filesystem,
   enabling cross-type retrieval in a single query.
4. **Drop everything the model makes unnecessary** — soft-delete, append-only
   archives, a sync engine, a lifecycle event log, urgency formulas, multi-agent
   claim coordination.

The result is a smaller, more interoperable, more agent-native tool whose value
is *retrieval over your markdown*, not *task management with a CLI*.

## The model

Every captured concept — a ticket, a note, a decision, a meeting, a tag — is a
**node** in a typed graph; relationships are **edges**. That is the whole
modeling vocabulary.

- **Nodes are markdown — or HTML — files.** Any `.md` file with a `type:` field
  in YAML frontmatter is a node; its workspace-relative path (minus the `.md`
  extension) is its canonical id. There is no separate id field. `.html`/`.htm`
  files that declare `<meta name="tusk:type">` are first-class nodes too: an HTML
  pass extracts DOM text as indexable prose and `data-*` attributes as signals,
  mirroring the markdown sub-unit pass under a `source = "html"` namespace (HTML
  ids retain their extension). `tusk node render` and the paired `tusk_node_render`
  MCP tool return any node's content as plain text.
- **Edges are typed and declared.** Frontmatter keys are either properties or
  edges; the manifest decides which, and enforces edge legality (`from`/`to`
  types, cardinality, ordering, acyclicity). Body wikilinks materialize
  navigational edges.
- **The manifest is the contract.** Node types, edge types, property schemas,
  and behavior packs are declared in `tusk.toml`. Type packs (`vault`, `tags`,
  `kanban`, `dev`) splice in curated bundles so you do not declare everything by
  hand.
- **Behaviors are opt-in engine logic.** v1 ships one behavior pack —
  `workflow`, a status finite-state machine validated on writes. A workspace
  with no packs is still a valid Tusk workspace: a markdown vault with retrieval.

See the [`README`](README.md) for the working file format, manifest examples,
and the query grammar.

## Architecture principles

Three layers: the **filesystem** (source of truth, in git), the **engine** (a
single Go binary — manifest loader, node/edge writer, file watcher, query
compiler, embedding pipeline), and a **local index** (`.tusk/`, gitignored,
SQLite + vectors).

- **Filesystem > index, always.** The index is a cache; if it is stale, wedged,
  or corrupt, run `tusk reset` (or the `tusk_reset` MCP tool with `confirm: true`)
  to drop and rebuild it from your files (modulo embedding cost). The markdown
  files are the source of truth, so nothing is lost.
- **External edits are first-class.** vim, Obsidian, ripgrep, an LLM piping
  markdown to disk — all work without going through the engine. The watcher
  keeps the index live; one-shot CLI users run `tusk reindex`.
- **Off-schema content is warned, not rejected.** A file that violates the
  manifest is still indexed and queryable; `tusk doctor` surfaces the violation.
  The engine never deletes, rejects, or silently mutates user content.
- **Stateless across machines.** The binary holds no per-installation state.
  Clone a vault, run `tusk reindex`, get an identical brain.
- **Node + edge are the only modeling primitives.** Anything else is config or
  behavior. Multi-agent coordination is a workspace-multiplicity problem
  (isolated workspaces merged via git), not an engine problem.

## Surfaces

One engine, two surfaces, with type-pack ergonomics on top:

- **CLI** — `tusk init`, `tusk reindex`, `tusk reload`, `tusk doctor`,
  `tusk status`, `tusk node …`, `tusk edge …`, `tusk query …`, plus pack
  shortcuts (`tusk ticket open`, `tusk note new`).
- **MCP server** — every CLI verb maps 1:1 to a `tusk_<noun>_<verb>` tool over
  stdio or SSE, so any MCP-compatible agent (Claude Code, Cursor, …) shares the
  same engine.

## Retrieval

Three query shapes, all over the same index:

- **Structural** — a TaskWarrior-flavored filter grammar (property predicates,
  comparators, ranges, edge traversal, boolean composition) compiled to
  parameterized SQL. No embedding cost.
- **Semantic** — the query is embedded (Ollama by default, with OpenAI / Voyage
  / Anthropic as first-class fallbacks) and ranked by nearest-neighbor over the
  embeddings table.
- **Hybrid (recommended for agents)** — a structural filter narrows the
  candidate set; semantic similarity ranks within it.

## Scope

What is shipped, what is deferred, and what is explicitly out of scope are
tracked in [`docs/roadmap.md`](docs/roadmap.md). The roadmap is the live source
for forward-looking work; this document describes the product's shape and intent.
