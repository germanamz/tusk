---
type: note
title: Tusk roadmap
---

# Tusk roadmap

Status snapshot and forward-looking backlog. Items here are **framings**, not
designs: each graduates into its own
`docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md` when it's time to
brainstorm and plan it.

- **Last updated:** 2026-06-05
- **Latest release:** v1.2.0
- **Closed specs/plans:**
  - v1 ground-up rewrite (shipped through 1.0/1.1) — design folded into [`PRODUCT.md`](../PRODUCT.md); original spec preserved in git history
  - CLI docs (`man/` + `docs/cli/`) — shipped in #393
  - v1.1 bug backlog (5 bugs surfaced bootstrapping the Superhuman workspace) — shipped in v1.2.0 via #397, #399, #400, #401, #402

## Forward-looking explorations

Each is independent in execution; the order reflects dependencies and
risk-adjusted payoff. Items #1–#3 capture index/manifest lifecycle ergonomics
and HTML-content support, raised 2026-06-05. Item #9 is a smaller ergonomic ask
promoted from the Superhuman bootstrap session.

1. [Index reset and rebuild](#1-index-reset-and-rebuild)
2. [Hot manifest reload](#2-hot-manifest-reload)
3. [HTML content AST](#3-html-content-ast)
4. [Native-Go embedding models](#4-native-go-embedding-models)
5. [Indexed checkbox todos in nodes](#5-indexed-checkbox-todos-in-nodes)
6. [CI-distributed prebuilt indexes](#6-ci-distributed-prebuilt-indexes)
7. [Paragraph indexing with local summarization](#7-paragraph-indexing-with-local-summarization)
8. [Distributed indexing](#8-distributed-indexing)
9. [Depth-N descendants in one query](#9-depth-n-descendants-in-one-query)

Below the focus items, the [v1 deferred backlog](#v1-deferred-backlog) lists
work parked in the v1 spec (§3.2 / §8.2) that's still on the table but
unscheduled.

---

## 1. Index reset and rebuild

**Problem.** The blessed recovery move when the index is stale, corrupt, or
schema-drifted is `rm -rf .tusk/ && tusk reindex` — documented in the README
architecture notes. But it lives only as shell folklore: there's no first-class
command, and agents driving Tusk over MCP can't shell out to `rm -rf` at all.
When an agent suspects a bad index, it has no in-band way to drop and rebuild.

**Why now.** Pure quality-of-life with a real correctness angle — an agent that
can self-heal a wedged index recovers without a human dropping to a terminal.
The underlying operation already exists and is safe by design (filesystem is
authoritative; the index is a cache), so this is mostly surface.

**Sketch.** New command + matching MCP tool that (1) removes the `.tusk/` index
artifacts (DB + WAL/SHM), (2) triggers a full reindex, and (3) returns the same
summary `tusk reindex` does. Guard with a confirmation flag on the CLI
(`--yes`) and make the MCP tool explicit about what it destroys. Keep it
distinct from `reindex`, which never deletes.

**Open questions.**
- Naming. Candidates floated: `restart`, `reset`, `rebuild`. `restart` collides
  conceptually with #2 (manifest reload) and with "restart the server"; `reset`
  reads closest to the destroy-and-rebuild semantics. Needs brainstorm.
- Blast radius: drop the whole `.tusk/` directory, or only the index DB +
  sidecars (preserving any future cached artifacts / config under `.tusk/`)?
- MCP safety: should the tool require an explicit `confirm: true` arg given it
  destroys local state?

**Dependencies.** None. Reuses the existing reindex path.

## 2. Hot manifest reload

**Problem.** `tusk.toml` is the schema. Today, editing it to add a node-type or
edge-type means the long-running `tusk mcp serve` process holds a stale manifest
until it's restarted — yet the MCP guidance tells agents to "edit ./tusk.toml
directly … then call tusk_reindex." Reindex re-reads *content*; it isn't a
contract that the in-memory manifest is reloaded. Agents that evolve the schema
mid-session have to ask a human to bounce the server.

**Why now.** Removes the one remaining "go restart the daemon" step from the
agent loop. Schema evolution is a normal part of bootstrapping a workspace, and
the long-lived MCP server is exactly where the stale-manifest cost is felt.

**Sketch.** New command + matching MCP tool that re-reads `tusk.toml`,
re-validates it, swaps the in-memory manifest atomically, and reports what
changed (added/removed node-types, edge-types, behaviors). On validation
failure, keep the old manifest loaded and return the error. Optionally fold this
into the watcher so a `tusk.toml` write auto-reloads, with the explicit
command/tool as the deterministic path for agents.

**Open questions.**
- Auto-reload via the watcher vs. explicit-only. Auto is ergonomic but a
  half-saved `tusk.toml` could transiently fail validation; explicit is
  predictable. Probably ship explicit first, watcher-driven later.
- Reload vs. reindex interaction: does a schema change that invalidates existing
  nodes (e.g. a removed node-type) trigger a reindex, or just re-validate and
  surface conflicts?
- Concurrency: reloading under the single-writer lock while a reindex is in
  flight.

**Dependencies.** None blocking. Pairs naturally with #1 (both are
server-lifecycle ergonomics) but ships independently.

## 3. HTML content AST

**Problem.** Tusk indexes markdown. Content that arrives as HTML — pasted
fragments, scraped pages, exported docs — is either skipped or indexed as raw
markup, so the embeddings carry tag soup (`<div class=...>`) instead of the
prose, and structured signals living in `data-*` attributes are invisible to
the graph.

**Why now.** Widens the corpus Tusk can usefully retrieve over without inventing
a new node type — HTML is a common interchange format for the notes and
references that land in a vault. `data-*` attributes in particular often carry
exactly the structured metadata we'd otherwise ask a user to hand-enter.

**Sketch.** A new HTML AST pass (mirroring the markdown pass) that parses HTML
content and extracts (1) DOM text nodes as the indexable prose and (2) `data-*`
attributes as candidate properties/signals. Plus — possibly — a command + MCP
tool that renders a node's (or a file's) content as plain text, stripping tags
so an agent can pull "just the words" without the HTML bloat.

**Open questions.**
- Parser choice: `golang.org/x/net/html` (std-adjacent, robust) vs. a lighter
  tokenizer. Lean toward `x/net/html`.
- Which attributes become first-class — only `data-*`, or also semantic ones
  like `id`, `class`, `href`, `src`, `alt`, `title`, `role`/`aria-*`? All of a
  chosen set, or a configured allowlist? Risk of attribute noise polluting the
  property space.
- Scope: a new content *type* alongside markdown, or a preprocessing step that
  normalizes HTML → text/properties before the existing markdown pipeline?
- Does the plain-text extractor belong here, or is it a general "render node as
  plain text" utility that also helps markdown consumers?

**Dependencies.** None. Composes with the existing chunking strategies (#7) once
HTML content is normalized to text.

## 4. Native-Go embedding models

**Problem.** Today every new user must install Ollama, pull a model, and keep
`ollama serve` running before `tusk reindex` can produce embeddings. This is
the single biggest install-friction point. The v1 spec parked a "bundled local
embedding model (ONNX)" item as out of scope; this entry supersedes it with a
different bet — pure-Go inference, no CGo, no ONNX runtime dependency.

**Why now.** The Ollama wall converts trial users into bounce-aways. A small
encoder (e.g. MiniLM-class, ~25M params) running natively in Go is enough for
useful semantic retrieval on personal vaults and pays for itself in onboarding
conversion alone.

**Sketch.** Add a `provider = "native"` option that loads a Go-implemented
encoder. Keep Ollama and the API providers (OpenAI, Voyage, Anthropic) as
first-class fallbacks. Provider becomes part of `embedding_content_hash` so a
provider switch correctly invalidates.

**Open questions.**
- Which model(s) to bundle. Tradeoff: dim/quality vs. binary size and inference
  speed in pure Go.
- Tokenizer story. SentencePiece / WordPiece in pure Go is doable but rarely
  pre-packaged at production quality.
- How much of `gomlx` / `go-llama` / hand-rolled tensor code do we depend on.
- Binary size budget — current `tusk` is small; adding a model bloats it.
  Option: ship a separate `tusk-models` companion binary, or fetch on first run.

**Dependencies.** None blocking. Unblocks #6 (#6's runtime story) and #7 (#7
benefits from a local model identity that survives release).

## 5. Indexed checkbox todos in nodes

**Problem.** GitHub-flavored markdown checkboxes (`- [ ]`, `- [x]`) inside
node bodies are invisible to the index today. Users (and agents) can't ask "show
me open todos across all nodes tagged `auth`" or "toggle the third todo in this
ticket without rewriting the whole file."

**Why now.** It's a small, well-scoped feature with clear payoff: tusk gains
fine-grained actionable state inside nodes without inventing a new node type.
Pairs naturally with the kanban pack.

**Sketch.** A markdown pass during indexing extracts checkbox lines with stable
identity (line offset + text hash, or a sibling-index path). New `todos` table
keyed by `(node_id, todo_id)` with `text`, `done`, `line_offset`. Filter grammar
gains `todo:open`, `todo:done`, `has-todo`. CLI/MCP gains `tusk todo list`,
`tusk todo check <node-id> <todo-id>`, `tusk todo uncheck`. Check toggles
rewrite the markdown atomically (same lock as `node modify`).

**Open questions.**
- Identity scheme. Line offset breaks on edits; hash collides; sibling-index
  path is stable but verbose. Probably hybrid: hash + offset as tiebreaker.
- Nested checkbox semantics (parent done iff all children done?). Likely no —
  keep it flat in v1.
- Do edges-to-todos make sense (`blocked-by:node-x/todo-3`)? Probably not —
  todos are intra-node, edges are inter-node.

**Dependencies.** None. Could ship in parallel with anything.

## 6. CI-distributed prebuilt indexes

**Problem.** Bootstrapping a new vault from a published corpus (e.g. the Tusk
docs, a curated reference set) means re-running the full embed pipeline locally.
For a 10k-node vault that's minutes of CPU and an Ollama install. If CI already
built the index for a release, users should be able to download it.

**Why now.** Lets us ship "instant-on" knowledge vaults — `tusk init
--bootstrap-from https://github.com/.../releases/.../vault-index.tar.zst` and
the user is querying in seconds. Also a forcing function on provider/model
identity hygiene (you can't safely re-open a published index without it).

**Sketch.** New CI step builds `.tusk/index.db` for a designated corpus,
publishes it as a release asset with a manifest (`embedding-provider`,
`embedding-model`, `dim`, `corpus-checksum`, `index-schema-version`, signature).
`tusk init --bootstrap-from <url>` downloads, verifies, and installs. `tusk
doctor` warns if the user's manifest's embedding config diverges from the
bootstrapped index.

**Open questions.**
- Signature scheme (sigstore / minisign / GPG / none-with-checksum-only).
- What happens when a user pulls newer markdown but keeps the old index — drift
  detection should already catch this; verify.
- Should the bootstrapped index be authoritative or replaceable on first
  `reindex --force`?

**Dependencies.** #4 (a native embedder makes "user can open a published index"
not require an external model server). Could ship with Ollama-only as a first
cut, then expand.

## 7. Paragraph indexing with local summarization

**Problem.** Today retrieval returns whole nodes (or `MarkdownRecursive` chunks,
shipped in #372/#376). For long nodes, the agent gets back too much text and
has to do another round trip to narrow in. We want tusk to hand the agent
*pre-distilled* relevant context.

**Why now.** Quality refinement on top of the chunking work already in. The
specific bet: paragraph-level chunks plus a per-chunk summarization pass using
a small local decoder model (Gemma-class, Phi-class) so retrieval returns
"short, on-topic, ready to consume." Reduces agent round trips per query.

**Sketch.** Add a `paragraph` chunking strategy (extends the existing
`ChunkingStrategy` interface — already designed for this in v1 §10.7). Optional
`[embeddings] summarize = true` config: when set, an LLM provider (mirroring
the embedding provider abstraction) generates a one-sentence summary per chunk.
Summary is stored alongside the chunk and returned in query results as
`snippet`. Summary regen tracks its own content hash separately from embedding
hash so summary-prompt or summary-model changes can re-run without re-embed.

**Open questions.**
- Provider abstraction for summarization — separate from embedding, or unified?
  Different runtimes (decoder vs encoder), different cost profiles.
- When to summarize: at index time (latency hit, cached forever) or at query
  time (latency hit per query, no storage). Probably index-time with cache.
- Cost / quality of small models for summarization. Gemma 2B class is plausible
  locally; smaller models often hallucinate.
- Interaction with #4 — does the native-Go story extend to decoder inference,
  or stay encoder-only?

**Dependencies.** #4 (clarifies how we run local models). Builds on existing
`MarkdownRecursive` chunker.

## 8. Distributed indexing

**Problem.** A single logical vault sharded across multiple machines or agents.
Hard primarily because of **rebalancing**: as nodes are added/removed, shard
assignment must stay consistent without recomputing every embedding. Vector
indexes resist rebalancing more than structural ones.

**Why now.** No immediate single-machine pain motivating this; capturing now so
the architecture admits it without retrofit. Treat as research-grade until a
concrete user need lands.

**Possible directions** (research, not commitments).
- **Hash-shard with merge protocol.** Each shard owns a stable hash slice of
  node-id space. Adding a shard splits a slice; merging recombines. Vector
  search fans out to all shards and merges top-k. Rebalancing cost is
  proportional to slice transfer, not whole-corpus rebuild.
- **Git-for-indexes.** Each machine maintains a local index; periodic
  delta-merge protocol (CRDT-friendly index format) reconciles. Read-only
  followers pull deltas. No central coordinator.
- **Cross-workspace federation** (lighter cousin, already in v1 spec §3.2). Not
  a sharded vault — multiple local vaults queried in parallel. Likely the
  first useful step toward (#8) without committing to the full distributed
  story.

**Open questions.**
- Is this actually wanted? Cross-workspace federation may satisfy 80% of the
  itch at 10% of the cost.
- Embedding consistency across shards (same provider+model, or per-shard
  metadata?).
- Failure modes — partial reads, stale shards, write conflicts.

**Dependencies.** None blocking, but worth seeing #4, #6, and #7 land first
since they sharpen what a "shard" carries.

## 9. Depth-N descendants in one query

**Problem.** Traversing a parent/child tree from the public surface requires
N round trips, one per level. The binary already carries `descendants_%d`
query strings internally — the SQL is parameterized by depth — but only
single-hop edge filters are exposed through `tusk_query` and the filter
grammar.

**Why now.** Felt during the Superhuman WBS workspace, where a Story has 2–4
levels of descendants below it. Agents that want "the whole subtree" pay N×
latency today for what's effectively one prepared statement.

**Sketch.** Promote the internal depth-parameterized traversal to the public
query surface. Filter grammar gains a descendants traversal (shape TBD —
e.g. `descendants(<edge-type>, depth=N)`), and `tusk_query` accepts the same
depth knob. Reuse the existing SQL machinery.

**Open questions.**
- Filter-grammar shape — function-call style, or a new operator?
- Default depth cap to prevent runaway traversals.
- Direction (parent → child, child → parent, both) — separate verbs or one
  with a direction arg?
- Result shape: flat list, or grouped by depth?

**Dependencies.** None — backing query strings already exist.

---

## v1 deferred backlog

From the v1 design spec's out-of-scope and future-behavior sections (folded into
[`PRODUCT.md`](../PRODUCT.md); full text in git history) — parked but still on
the table. Reconciled against the five focus items above.

| Item | Notes |
|---|---|
| HTTP API | MCP-only in v1; v1.x candidate when a real consumer surfaces. |
| `crm` type pack (`person`, `company`, `interaction`) | v1.x; behind a real user signal. |
| Auto-transition rules (parent auto-completes when children done) | Manifest already accepts the option as a no-op. |
| `due-reminders` behavior pack | Local notifications when due dates approach. |
| `recurring` behavior pack | Auto-create instances of a template node on a schedule. |
| `vector-watcher` behavior pack | Re-embed when content drifts substantially. |
| Cross-workspace queries | Lighter cousin of #8 (Distributed indexing). May land first as a stepping stone. |
| Plugin loading for behavior packs | v2+. |
| Web UI / TUI | v2+. |

**Superseded:**

- *Bundled local embedding model (ONNX)* → replaced by #4 (Native-Go embedding
  models). Different bet: pure-Go runtime, no ONNX dependency.

---

## Process

When picking up an item:

1. Run brainstorming on that item alone — produces
   `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`.
2. Run writing-plans on the spec — produces matching file under
   `docs/superpowers/plans/`.
3. Implement on a feature branch in-place (no worktree).
4. Update this file: move the item to the "Closed specs/plans" list at the top
   when it ships.
