---
type: note
title: Tusk roadmap
---

# Tusk roadmap

Status snapshot and forward-looking backlog. Items here are **framings**, not
designs: each graduates into its own
`docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md` when it's time to
brainstorm and plan it.

- **Last updated:** 2026-05-17
- **Latest release:** v1.1.0
- **Closed specs/plans:**
  - `specs/2026-05-05-tusk-v1-rebuild-design.md` — v1 ground-up rewrite (shipped through 1.0/1.1)
  - `specs/2026-05-16-tusk-cli-docs-design.md` + matching plan — `man/` and `docs/cli/` shipped in #393

## Top priority — v1.1 bug backlog

Five bugs surfaced while bootstrapping the Superhuman workspace against
v1.1.0. Full repros, expected behavior, suggested fixes, and severity in
`docs/bugs.md`. These ship before any of the forward-looking explorations
below.

Filing / fix order (lowest-coupling first, per `docs/bugs.md`):

1. **Filter docs say `key:value`, parser requires `key=value`** — docs drift, or parser accepts both. (Low severity, trivial.)
2. **Workflow-owned `status` triggers permanent `undeclared-property` warning** — affects the built-in `kanban` pack too. (Low / validator fix.)
3. **`ordered=true` edges all get ordinal 0 via `tusk edge add`** — no `--ordinal` flag; auto-assign or accept one. (Low / design clarity.)
4. **`tusk node move` drops the file extension when the target has none** — desyncs index from disk until manually repaired. (Medium.)
5. **`:` in a string property silently breaks the file on write** — frontmatter writer needs YAML-safe string emission. (High — silent corruption; deferred behind the easier ones to let a pattern emerge.)

Two ergonomic asks ride along, not bugs but felt during the same bootstrap:
composite `tusk_node_attach` and depth-N descendants in a single query.

## Forward-looking explorations

Suggested priority for new feature work *after* the bug backlog clears. Each is
independent in execution; the order reflects dependencies and risk-adjusted
payoff.

1. [Native-Go embedding models](#1-native-go-embedding-models)
2. [Indexed checkbox todos in nodes](#2-indexed-checkbox-todos-in-nodes)
3. [CI-distributed prebuilt indexes](#3-ci-distributed-prebuilt-indexes)
4. [Paragraph indexing with local summarization](#4-paragraph-indexing-with-local-summarization)
5. [Distributed indexing](#5-distributed-indexing)

Below the five focus items, the [v1 deferred backlog](#v1-deferred-backlog) lists
work parked in the v1 spec (§3.2 / §8.2) that's still on the table but
unscheduled.

---

## 1. Native-Go embedding models

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

**Dependencies.** None blocking. Unblocks #3 (#3's runtime story) and #4 (#4
benefits from a local model identity that survives release).

## 2. Indexed checkbox todos in nodes

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

## 3. CI-distributed prebuilt indexes

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

**Dependencies.** #1 (a native embedder makes "user can open a published index"
not require an external model server). Could ship with Ollama-only as a v0,
then expand.

## 4. Paragraph indexing with local summarization

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
- Interaction with #1 — does the native-Go story extend to decoder inference,
  or stay encoder-only?

**Dependencies.** #1 (clarifies how we run local models). Builds on existing
`MarkdownRecursive` chunker.

## 5. Distributed indexing

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
  first useful step toward (#5) without committing to the full distributed
  story.

**Open questions.**
- Is this actually wanted? Cross-workspace federation may satisfy 80% of the
  itch at 10% of the cost.
- Embedding consistency across shards (same provider+model, or per-shard
  metadata?).
- Failure modes — partial reads, stale shards, write conflicts.

**Dependencies.** None blocking, but worth seeing #1, #3, and #4 land first
since they sharpen what a "shard" carries.

---

## v1 deferred backlog

From `specs/2026-05-05-tusk-v1-rebuild-design.md` §3.2 and §8.2 — parked but
still on the table. Reconciled against the five focus items above.

| Item | Notes |
|---|---|
| HTTP API | MCP-only in v1; v1.x candidate when a real consumer surfaces. |
| `crm` type pack (`person`, `company`, `interaction`) | v1.x; behind a real user signal. |
| Auto-transition rules (parent auto-completes when children done) | Manifest already accepts the option as a no-op. |
| `due-reminders` behavior pack | Local notifications when due dates approach. |
| `recurring` behavior pack | Auto-create instances of a template node on a schedule. |
| `vector-watcher` behavior pack | Re-embed when content drifts substantially. |
| Cross-workspace queries | Lighter cousin of #5 (Distributed indexing). May land first as a stepping stone. |
| Plugin loading for behavior packs | v2+. |
| Web UI / TUI | v2+. |
| Migration tool from v0.x | Likely dead — no demand signal since v1.0. Drop if still quiet by v1.3. |

**Superseded:**

- *Bundled local embedding model (ONNX)* → replaced by #1 (Native-Go embedding
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
