---
type: note
title: Tusk roadmap
---

# Tusk roadmap

Status snapshot and forward-looking backlog. Items here are **framings**, not
designs: each graduates into its own
`docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md` when it's time to
brainstorm and plan it.

- **Last updated:** 2026-06-27
- **Latest release:** v1.17.0
- **Closed specs/plans:**
  - HTML content AST (`.html`/`.htm` files as first-class nodes + `tusk node render` / `tusk_node_render`) — shipped; design folded into [`PRODUCT.md`](../PRODUCT.md) and [`README`](../README.md).
  - Indexed checkbox todos in nodes — shipped, but delivered through the sub-unit indexing model (`internal/subunit`) rather than the originally-sketched dedicated `todos` table / `tusk todo` command. Markdown list-item sub-units carry a `checkbox` property, so open/done todos are filterable and queryable cross-source through the standard query surface. Original framing preserved in git history.
  - v1 ground-up rewrite (shipped through 1.0/1.1) — design folded into [`PRODUCT.md`](../PRODUCT.md); original spec preserved in git history
  - CLI docs (`man/` + `docs/cli/`) — shipped in #393
  - v1.1 bug backlog (5 bugs surfaced bootstrapping the Superhuman workspace) — shipped in v1.2.0 via #397, #399, #400, #401, #402
  - Index reset and rebuild (`tusk reset` + `tusk_reset`) — shipped; design in git history.
  - Hot manifest reload (`tusk reload` + `tusk_reload`) — shipped via #535, #537–#544 + docs; a `.tusk/manifest-epoch` sentinel drives cross-process schema convergence, layered on the index-reset work. Design preserved under `docs/superpowers/` (local).
  - Graph view (`tusk graph`, three.js) — shipped. A local HTTP server serves a self-contained page that renders the index as an interactive 3D force-directed graph over a read-only JSON endpoint (#567, v1.13.0), since enriched with degree-based sizing/coloring, a cluster lens (group color, huddle layout, hull overlays; #586–#598), and a semantic layout mode where position tracks embedding similarity via umap-js (#600–#603). The framing's open questions (2D/3D toggle, in-page query, live updates) resolved during implementation; behavior documented in [`docs/architecture/graph-view.md`](architecture/graph-view.md) and [`docs/packages/graphview.md`](packages/graphview.md).

## Forward-looking explorations

Each is independent in execution. The order now leads with language-aware code
indexing (#1–#3), followed by the earlier backlog (#4–#8), whose internal order
still reflects dependencies and risk-adjusted payoff. Item #8 is a smaller
ergonomic ask promoted from the Superhuman bootstrap session.

1. [Go indexer pass](#1-go-indexer-pass)
2. [TypeScript/JavaScript indexer pass](#2-typescriptjavascript-indexer-pass)
3. [Python indexer pass](#3-python-indexer-pass)
4. [Native-Go embedding models](#4-native-go-embedding-models)
5. [CI-distributed prebuilt indexes](#5-ci-distributed-prebuilt-indexes)
6. [Paragraph indexing with local summarization](#6-paragraph-indexing-with-local-summarization)
7. [Distributed indexing](#7-distributed-indexing)
8. [Depth-N descendants in one query](#8-depth-n-descendants-in-one-query)

Below the focus items, the [v1 deferred backlog](#v1-deferred-backlog) lists
work parked in the v1 spec (§3.2 / §8.2) that's still on the table but
unscheduled.

---

## 1. Go indexer pass

**Problem.** Tusk indexes markdown (and now HTML) but treats a source tree as
opaque. A vault that lives next to a Go codebase can't answer "where is
`parseConfig` called," "what imports this package," or "what does this function
*mean*" — the code is invisible to retrieval, and agents working in the repo fall
back to grep and full-file reads. Fitting to open the language passes with Go,
since `tusk` is itself a Go codebase and would become self-describing in its own
vault.

**Why now.** Go has the cleanest parse story of the three: `go/parser` and
`go/ast` are in the standard library, already in-process, and need no external
toolchain or CGo. Doc-comment conventions are rigid and well-specified, so
docstring extraction is unusually reliable. That low-risk, in-process surface
makes Go the right place to establish the language-pass pattern the other
languages reuse — the sub-unit + structural-address machinery (shipped for
markdown and HTML) generalizes to any parseable source: index a file as a node,
emit sub-units for the structures inside it, attach edges for the relations
between them.

**Sketch.** A language pass parses each `.go` file and emits:
- **Symbol-usage edges** — calls, assignments, references, `import` paths, and
  file/package relations — so the graph captures who-calls-what and
  who-imports-what.
- **Doc-comment sub-units** — documentation comments attached to the symbol they
  describe.
- **Plain-comment sub-units** — inline and block comments that aren't doc comments.

Crucially, **the indexer model never sees raw code.** Embedding source bodies
would flood the vector index with dense, low-signal tokens (syntax, boilerplate,
identifiers) and balloon its size for little retrieval gain. Instead Tusk indexes
the *ideas* — comments and doc comments carry intent in natural language — while
symbols and their relations are captured structurally as edges, not embeddings.
Native `go/ast` gives precise positions and comment-to-declaration mapping for
stable sub-unit addressing, and package-level structure adds a relation layer the
other two languages lack.

**Open questions.**
- Granularity: package as node, file as node, or both (Go's package = directory
  model doesn't map 1:1 to one-file-one-node).
- Whether to use `go/types` for full symbol resolution (accurate cross-file
  edges, but needs a buildable module) or stay at the syntactic `go/ast` level.
- Build-tag and generated-file handling.
- Edge-type vocabulary: do `calls`, `imports`, `references`, `assigns` live in
  `tusk.toml` as first-class edge types, or a generic `code-ref` with a subtype?

**Dependencies.** Builds directly on the sub-unit / structural-address work
(`internal/subunit`) and the source-parameterized Sync added for the HTML AST.
Establishes the language-pass pattern — edge vocabulary, comment-to-sub-unit
addressing, and the "comments-not-code" indexing model — that #2 and #3 reuse.
Lowest implementation risk of the three thanks to the stdlib parser, which makes
it the right place to prove the framework.

## 2. TypeScript/JavaScript indexer pass

**Problem.** Same gap as #1, for TS/JS codebases — a vault that lives next to a
TS/JS tree can't answer "where is `parseConfig` called," "what imports this
module," or "what does this function *mean*"; the code is invisible to retrieval
and agents fall back to grep and full-file reads.

**Why now.** TS/JS is the highest-leverage language for this work — it's where
most agent-assisted development happens, and tree-sitter / the TypeScript
compiler API give a mature parse surface to apply the framework #1 establishes.

**Sketch.** The #1 model applied to `.ts`/`.tsx`/`.js`/`.jsx` files: symbol-usage
edges (function/method calls, variable assignments and references,
`import`/`export` relations, file-to-file dependencies), JSDoc/docstring
sub-units, and plain-comment sub-units — and, as in #1, **no raw code is
embedded.** Retrieval answers "what was the author thinking here" from comments
and "what connects to what" from the graph.

**Open questions.**
- Parser choice: tree-sitter (fast, uniform across languages, weaker type info)
  vs. the TypeScript compiler API (richer symbol resolution, JS-runtime
  dependency, TS-centric).
- Symbol identity across files — fully-resolved vs. heuristic name matching for
  call/reference edges.
- How comments map to sub-unit addresses so they stay stable across edits, the
  way markdown sub-units already do.

**Dependencies.** #1 (reuses the language-pass framework, edge vocabulary, and
"comments-not-code" indexing model), applied to the richer, messier JS/TS parse
surface.

## 3. Python indexer pass

**Problem.** Same gap as #1, for Python codebases: imports, call graphs,
references, and docstrings are invisible to the index.

**Why now.** Python's first-class docstring convention (module/class/function
`"""..."""`) makes the "index ideas, not code" bet especially strong — a large
share of intent is already written down in a structured place the pass can lift
directly.

**Sketch.** The same model as #1 applied to `.py` files: symbol-usage edges
(calls, assignments, references, `import` / `from ... import`, file relations),
docstring sub-units, and plain-comment sub-units — and, as in #1, **no raw code
is embedded.** Language-specific deltas: Python docstrings are first-class AST
nodes (not comments), so the pass reads them straight from the parse tree;
dynamic imports and re-exports complicate the dependency graph.

**Open questions.**
- Parser: tree-sitter-python vs. the stdlib `ast` (would require a Python
  runtime alongside `tusk`) vs. a pure-Go Python parser.
- Decorators and dynamic attribute access — how much of the call graph to
  attempt statically before it stops being reliable.
- Type-hint signal: index annotations as structured metadata, or ignore in v1?

**Dependencies.** #1 (shares the language-pass framework, edge vocabulary, and
"comments-not-code" indexing model).

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

**Dependencies.** None blocking. Unblocks #5 (its runtime story) and #6 (#6
benefits from a local model identity that survives release).

## 5. CI-distributed prebuilt indexes

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

## 6. Paragraph indexing with local summarization

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

## 7. Distributed indexing

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
  first useful step toward (#7) without committing to the full distributed
  story.

**Open questions.**
- Is this actually wanted? Cross-workspace federation may satisfy 80% of the
  itch at 10% of the cost.
- Embedding consistency across shards (same provider+model, or per-shard
  metadata?).
- Failure modes — partial reads, stale shards, write conflicts.

**Dependencies.** None blocking, but worth seeing #4, #5, and #6 land first
since they sharpen what a "shard" carries.

## 8. Depth-N descendants in one query

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
the table. Reconciled against the focus items above.

| Item | Notes |
|---|---|
| HTTP API | MCP-only in v1; v1.x candidate when a real consumer surfaces. |
| `crm` type pack (`person`, `company`, `interaction`) | v1.x; behind a real user signal. |
| Auto-transition rules (parent auto-completes when children done) | Manifest already accepts the option as a no-op. |
| `due-reminders` behavior pack | Local notifications when due dates approach. |
| `recurring` behavior pack | Auto-create instances of a template node on a schedule. |
| `vector-watcher` behavior pack | Re-embed when content drifts substantially. |
| Cross-workspace queries | Lighter cousin of #7 (Distributed indexing). May land first as a stepping stone. |
| Plugin loading for behavior packs | v2+. |
| Web UI / TUI | v2+; the shipped graph view (`tusk graph`) is a narrow read-only slice; a general editing UI remains deferred. |

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
