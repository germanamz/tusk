# Agent Retrieval Improvements — Phase 3 (Graph-Aware Semantic Ranking) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Code blocks in this plan are **data-structure proofs only** — schemas, struct shapes, manifest examples, response envelopes. Implementation bodies are deliberately omitted; describe them in prose during execution.

**Spec:** `docs/superpowers/specs/2026-05-23-agent-retrieval-improvements-design.md` §6.

**Goal:** Add edge-aware re-ranking to semantic queries so that "auth" surfaces the OAuth-flow paragraph via graph proximity, not only via word match. Default off; per-workspace and per-call opt-in.

**Architecture:** Sits on top of the existing semantic ranker. The pipeline becomes: cosine top-K → walk N hops along configured edge types → re-rank candidates by blending cosine and graph-derived scores → truncate. No schema changes; the algorithm reads existing `embeddings` and `edges` tables. The configuration knob is a new `[query.graph-expansion]` manifest block, mirrored by per-call CLI/MCP flags. P3 is technically independent of P2 but produces materially better results when sub-units are present because the graph is finer-grained.

**Tech Stack:** Go, SQLite (existing `internal/index`), existing `internal/embed/cosine.go`, BurntSushi/toml.

**Prerequisites:** Phase 1 complete. Phase 2 is recommended but not strictly required; P3 ships either way.

---

## Inherits From

P3 builds on P1 and (optionally) P2.

- From P1: every read-verb routes through a `<Verb>Run` service function. The `query` service is the integration point; flags / MCP args / alias dispatch all share one path.
- From P1: filter grammar accepts `modified-since:` predicates.
- From P2 (when present): the candidate set for semantic queries is sub-unit embeddings; `contains` / `contained-by` edges link files to their sub-units. P3 walks these naturally without code changes.
- From P2 (when absent): the candidate set is file-level embeddings; the graph is sparser; P3 still works but with coarser results.

The implementer must support both scenarios. The branching is a function of `[workspace] sub-units` plus the `embeddings` table contents — no separate code paths needed because the graph walk uses whatever edges and nodes exist.

---

## Task 1: Manifest configuration block and per-call override flags

**Why this task exists:** Establish the configuration surface before plugging it into the query path. The block exists, parses, and validates — but does not yet affect query behavior. Subsequent tasks wire it in.

**Files:**

- Create: `internal/manifest/graph_expansion.go` — the `GraphExpansion` config struct, default values, validation.
- Create: `internal/manifest/graph_expansion_test.go`.
- Modify: `internal/manifest/manifest.go` to load `[query.graph-expansion]` into the workspace config.
- Modify: each Cobra `RunE` for read-verbs that take a semantic argument (just `query` in v1) to expose the per-call flags.
- Modify: `internal/mcp/tools.go` `tusk_query` registration to expose the per-call MCP arguments.
- Tests: TOML round-trip tests; default-value tests; validation tests for out-of-range values.

**Steps:**

- [ ] **Define the config struct.** Holds the four tunables plus the per-call overrides forwarded from CLI/MCP.

  Data-structure proof:

  ```go
  // internal/manifest/graph_expansion.go
  type GraphExpansion struct {
      Enabled             bool     // default false
      Hops                int      // 1 or 2; default 1
      EdgeTypes           []string // default ["references", "parent", "tagged", "contains"]
      Weight              float64  // 0.0..1.0; default 0.2
      CandidateMultiplier int      // K = top * multiplier; default 5
  }

  func DefaultGraphExpansion() GraphExpansion
  func (g GraphExpansion) Validate() []error
  ```

  And the on-disk TOML shape (verbatim from the spec):

  ```toml
  [query.graph-expansion]
  enabled              = false
  hops                 = 1
  edge-types           = ["references", "parent", "tagged", "contains"]
  weight               = 0.2
  candidate-multiplier = 5
  ```

- [ ] **Defaults.** When the block is absent entirely, `Enabled = false` and the rest are populated with the spec defaults. When the block is present but partial, missing fields fall back to defaults; explicitly-set fields override.

- [ ] **Validation.** `Hops ∈ {1, 2}`. `Weight ∈ [0.0, 1.0]`. `CandidateMultiplier ≥ 1`. Each `edge-types` entry must match a known edge type in the active manifest (after type-pack expansion); unknown edge types are warnings in doctor, not load errors — the user might be staging a future pack. Invalid `hops` or `weight` is a hard load error.

- [ ] **CLI flags on `tusk query`.**
  - `--graph-expand` (bool): turn graph expansion on for this call regardless of manifest setting.
  - `--no-graph-expand`: turn it off regardless of manifest.
  - `--hops <N>`: override `hops` (1 or 2).
  - `--graph-weight <float>`: override `weight`.
  - `--graph-edges <comma-separated>`: override `edge-types`.

- [ ] **MCP arguments on `tusk_query`.**
  - `graph_expand` (optional bool).
  - `hops` (optional int).
  - `graph_weight` (optional number).
  - `graph_edge_types` (optional array of string).

- [ ] **Forward the merged config to the query service.** In the `QueryRequest` struct from P1, add a `GraphExpansion *GraphExpansion` field. The CLI / MCP handler merges (manifest defaults) + (workspace `[query.graph-expansion]` block, if present) + (per-call overrides) into the request, in that precedence order. If `Enabled == false` after merge, the query service ignores the field.

- [ ] **Verification.** Round-trip a fixture `tusk.toml` with a `[query.graph-expansion]` block through the manifest loader; assert the resulting config struct. Run `tusk query --semantic "test" --graph-expand --hops 2` against a fixture and confirm the request struct shows `Enabled = true, Hops = 2`. (This task does not yet change query results — only the config plumbing.)

- [ ] **Commit.** `feat(query): configuration plumbing for graph expansion`.

**Pitfalls:**

- Be careful with the precedence: `--no-graph-expand` must beat a workspace `enabled = true`. Use a tri-state (nil / true / false) on the CLI flag so "unset" defers to manifest and "set" overrides.
- Unknown edge types in the list are common during pack changes (workspace was migrated, manifest still lists a pack-removed edge). Don't fail the query over this; just warn in doctor and skip the unknown name when walking.

---

## Task 2: Graph-walk implementation

**Why this task exists:** Given a candidate set of node IDs and an edge-type list, the engine must collect their N-hop neighbors with bounded cost. The walk is a SQL-side operation; the result is the augmented candidate set with neighbor relations.

**Files:**

- Create: `internal/graphexpand/walk.go` — the walker.
- Create: `internal/graphexpand/walk_test.go`.
- Modify: `internal/index/edge_repo.go` to expose a batch lookup: `NeighborsByEdgeTypes(ctx, sourceIDs, edgeTypes) ([]NeighborRow, error)`.
- Tests: walker tests over a fixture index with a small graph (~30 nodes, controlled edges).

**Steps:**

- [ ] **Define the walker.** A `Walker` operates on an `index.Store` and a list of edge types. It exposes a single method that takes the seed candidate set (with their cosine scores) and returns the augmented set.

  Data-structure proof:

  ```go
  // internal/graphexpand/walk.go
  type Candidate struct {
      NodeID      string
      CosineScore float64        // 0 for neighbors that were not in the initial top-K
      Distance    int            // 0 for seeds, 1 or 2 for hops
  }

  type Walker struct {
      Store     *index.Store
      EdgeTypes []string
      MaxHops   int
  }

  func (w *Walker) Expand(ctx context.Context, seeds []Candidate) ([]Candidate, []NeighborEdge, error)

  type NeighborEdge struct {
      Source string
      Target string
      Type   string
  }
  ```

  The `Distance` field on each candidate records which hop it was reached at; the re-ranker (Task 3) uses this to weight neighbors closer to the seed more heavily if desired.

- [ ] **Single-hop walk.** Run one batched SQL query: `SELECT source_id, target_id, type FROM edges WHERE type IN (...) AND (source_id IN (...) OR target_id IN (...))` with the seed IDs bound. Use prepared statements with the IN list size bounded by SQLite's parameter limit (default 32,766 — safe for any realistic candidate set).

- [ ] **Two-hop walk.** Repeat the same SQL with the union of (seeds ∪ first-hop-neighbors) as the source set, then dedupe. Bounded by `MaxHops = 2` per spec §6.2.

- [ ] **Neighbors collected with cosine scores.** A neighbor reached via the walk that was NOT in the initial cosine top-K is added with `CosineScore = 0`. A neighbor that WAS in the initial set keeps its cosine score.

- [ ] **Edge directionality.** The walk is undirected for graph-expansion purposes — `source_id IN (seeds) OR target_id IN (seeds)` captures both directions. The `inverse` mechanism on edges (e.g., `contained-by` is the derived inverse of `contains`) means an edge listed as `contains` is reachable from either endpoint via the SQL.

- [ ] **Test fixture.** Build a synthetic small graph in a fixture index: 5 file nodes connected by `references` edges, plus 10 sub-units (paragraph and section) connected by `contains` to the files. Run the walker with seeds = {file-1, file-3}, edge-types = ["references", "contains"], hops = 2; assert the returned candidate set matches the expected union of seeds and neighbors.

- [ ] **Verification.** Run `go test ./internal/graphexpand/...` — all passing.

- [ ] **Commit.** `feat(graphexpand): edge-type-aware N-hop walker`.

**Pitfalls:**

- Performance is the explicit P3 acceptance criterion (per spec §6.5). For K=50, hops=1, avg degree 5, the walk touches at most 250 edges — a single SQL query, sub-millisecond on a warm SQLite. Hops=2 fans out to ~K × degree² = 1,250 in worst case; still cheap. If profiling shows a hot spot, add a covering index on `edges(type, source_id, target_id)` — but the migration is out of scope here; recommend in doctor instead.
- The walker should be context-aware; long candidate sets shouldn't hold the workspace lock across the SQL. The query path is read-only and uses WAL, so contention is limited, but `ctx.Done()` should still short-circuit.

---

## Task 3: Re-ranking and blending into the query path

**Why this task exists:** This is where the algorithm becomes visible — cosine top-K candidates plus walked neighbors get re-scored with the blended formula and truncated to the user's requested `top`.

**Files:**

- Modify: `internal/query/...` (the query service from P1 Task 1) to invoke the walker and blender when graph expansion is enabled.
- Create: `internal/graphexpand/blend.go` — the blender that computes `graph_score` and `final_score`.
- Create: `internal/graphexpand/blend_test.go`.
- Modify: query result types to carry the breakdown (`cosine_score`, `graph_score`, `final_score`) for debuggability when `--explain` is set (see step below).
- Tests: blender tests with synthetic candidate sets; end-to-end query tests with graph expansion on a fixture vault.

**Steps:**

- [ ] **Augment the query pipeline.** The current semantic query path is roughly: parse filter → compile to SQL → fetch candidate IDs → embed query → cosine rank → truncate to `top` → render. The new path inserts two steps: after cosine rank, run the walker; after the walker, blend; then truncate and render.

  Pseudocode of the new shape (still prose, no implementation):
  1. Cosine rank the embeddings table, restricted to the structural-filter candidate IDs. Keep the top `K = top * candidate_multiplier` (default `K = top * 5`).
  2. Walker.Expand(seeds = top-K candidates with their cosine scores) → augmented candidates with distance and neighbor edges.
  3. Blender: for each candidate, compute `graph_score`; combine into `final_score = (1 - w) * cosine + w * graph_score`. The walker's `Distance` field is available to the blender; v1 uses it as a tie-breaker only.
  4. Sort by `final_score` desc; truncate to the requested `top`.

- [ ] **Blender contract.** Per spec §6.1: `graph_score` for a candidate is the sum of cosine scores of its neighbors that were in the initial top-K, normalized by the number of such neighbors. If a candidate has no neighbors in the top-K (e.g., it was reached by the walk but its own neighbors aren't otherwise prominent), `graph_score = 0`.

  Data-structure proof:

  ```go
  // internal/graphexpand/blend.go
  type Scored struct {
      NodeID      string
      Distance    int
      CosineScore float64
      GraphScore  float64
      FinalScore  float64
  }

  type Blender struct {
      Weight float64
  }

  func (b *Blender) Score(candidates []Candidate, edges []NeighborEdge) []Scored
  ```

- [ ] **Carry breakdown in the result for debuggability.** Add an `--explain` flag to `tusk query` that, when set, includes `cosine_score`, `graph_score`, `final_score`, and `distance` in each result row (as part of the existing JSON shape, omitted when `--explain` is off to keep payloads small). Surface in compact form too: `0.84 = 0.74×0.8 + 0.66×0.2`. This is essential for debugging when graph expansion produces surprising rankings.

- [ ] **Sub-units interaction.** When P2 is present, the cosine top-K is over sub-unit embeddings; the walker walks `contains` (so sub-unit → parent file) plus configured edges. The blender sees a mix of sub-unit IDs and file IDs in its candidate list. Section weighting from §5.7 (heading-level × max(leaf-score)) runs AFTER blending — it operates on the final per-leaf scores. Order: cosine → walk → blend → section-aggregate → group-by-file → truncate → render. Document this order in code comments.

- [ ] **End-to-end test.** Build a small fixture vault (5 notes, some with sub-units if P2 is landed) with controlled `references` edges. Run a semantic query whose top-K cosine results don't include node X, but where node X is referenced from multiple top-K nodes. With graph expansion off, X doesn't appear. With graph expansion on (hops=1, weight=0.3), X appears in the top results. This is the central acceptance test for P3.

- [ ] **Verification.** Full test suite green. Manual: `tusk query --semantic "auth" --graph-expand --explain --top 10` against a fixture vault; eyeball the score breakdown.

- [ ] **Commit.** `feat(query): graph-aware re-ranking via edge expansion`.

**Pitfalls:**

- Normalization: cosine scores are in [0, 1] (or sometimes [-1, 1] depending on the embedding model — verify). The blended `final_score` should stay in [0, 1]; if cosine produces negative values, clip or shift. Test edge cases.
- Don't double-count: if a candidate's neighbor is itself a candidate, the neighbor's contribution to the candidate's `graph_score` and vice versa are both legitimate — symmetry is fine. But avoid summing both directions of the same edge into one candidate's graph score; the walker should dedupe edges.
- When `candidate-multiplier × top` exceeds the number of embedded nodes in the workspace (small vault), the walker just gets every node. Performance stays bounded; behavior is correct.

---

## Task 4: Doctor surfacing and integration verification

**Why this task exists:** The configuration block has user-facing failure modes — unknown edge types, out-of-range values, weight=0 silently making the feature a no-op. Doctor surfaces these. This task is also the integration smoke for P3.

**Files:**

- Modify: `internal/doctor/...` to add the graph-expansion pane.
- Modify: man pages and CLI docs via `make docs`.
- Tests: doctor-output golden tests; an integration test that exercises P3 end-to-end.

**Steps:**

- [ ] **Doctor pane.** Add a "Graph Expansion" pane to `tusk doctor` reporting:
  - Whether `[query.graph-expansion] enabled = true` is set.
  - Whether each `edge-types` entry resolves to a known edge type in the active manifest. List unknowns as warnings (they're silently skipped at query time but indicate manifest drift).
  - The current `hops`, `weight`, `candidate-multiplier`.
  - When `weight = 0`, surface a soft warning: "graph expansion enabled but weight is 0 — feature is a no-op."

- [ ] **Integration test.** A `go test ./...` invocation that:
  - Builds a fixture vault with controlled edges.
  - Runs a semantic query without graph expansion; records the top-N.
  - Runs the same query with graph expansion on; asserts the top-N includes a node that was not in the original top-N (the central P3 behavior).
  - Runs `tusk doctor` and asserts the pane appears.

- [ ] **Regenerate CLI docs.** `make docs`. Commit the regenerated man pages and markdown CLI docs (per the docs-drift pre-push hook).

- [ ] **Verification.** Full Go test suite green. Manual integration: enable graph expansion in a real fixture vault, run a query, confirm the result set changes in the expected direction (more thematically-relevant results, sometimes at the cost of vocabulary-relevance).

- [ ] **Commit.** `feat(doctor): graph expansion pane and P3 integration verification`.

**Pitfalls:**

- The "graph expansion changed my results" outcome must be debuggable. The `--explain` breakdown from Task 3 is the user's escape hatch when graph expansion produces a surprising ranking. Make sure the doctor pane mentions `--explain` as the diagnostic next step.

---

## Self-Review

1. **Spec coverage.** §6.1 → Tasks 2, 3. §6.2 → Task 1. §6.3 → Task 1 (default off). §6.4 → handled implicitly by Tasks 2-3 (no separate code path needed). §6.5 → Task 2 (walker uses bounded SQL). ✓
2. **Placeholder scan.** No TBDs. The "verify cosine score range" note in Task 3 is a real concern the implementer must resolve; treat as a sub-step. ✓
3. **Type consistency.** `Candidate`, `NeighborEdge`, `Scored` types all defined in their introduction tasks and consumed consistently in later tasks. `GraphExpansion` config struct flows through P1's `QueryRequest`. ✓

---

## Changes Introduced

**New files:**
- `internal/manifest/graph_expansion.go` + `graph_expansion_test.go`
- `internal/graphexpand/walk.go` + `walk_test.go`
- `internal/graphexpand/blend.go` + `blend_test.go`

**Modified interfaces:**
- `internal/manifest/manifest.go` — loads `[query.graph-expansion]`.
- `internal/index/edge_repo.go` — adds `NeighborsByEdgeTypes` batch lookup.
- `internal/query/...` (created in P1) — inserts walker + blender between cosine and truncate.
- `tusk query` CLI — adds `--graph-expand`, `--no-graph-expand`, `--hops`, `--graph-weight`, `--graph-edges`, `--explain`.
- `tusk_query` MCP — adds `graph_expand`, `hops`, `graph_weight`, `graph_edge_types`, `explain` arguments.

**New environment variables:** none.

**Schema migrations:** none.

**Added dependencies:** none.

**Bridge code introduced:** none. P3 is fully additive and default-off.

**Manifest additions:**
- `[query.graph-expansion]` block.

**Doctor surfaces added:**
- "Graph Expansion" pane (current config, unknown edge type warnings, weight=0 no-op warning).

---

## User-Visible Behaviors That Must Still Work

After P3 is applied, the implementer agent confirms each of these:

- All P1 and P2 user-visible behaviors continue to work unchanged.
- A workspace without `[query.graph-expansion]` in `tusk.toml` produces identical semantic query results to current `main` (or to post-P2 if P2 is also applied) — graph expansion is off by default.
- Setting `[query.graph-expansion] enabled = true weight = 0.2` and running `tusk query --semantic "auth"` produces a different result set from `enabled = false`, surfacing graph-proximate nodes that pure cosine missed.
- `tusk query --semantic "..." --graph-expand` enables expansion for one call without manifest changes.
- `tusk query --semantic "..." --explain` shows the per-result breakdown (`cosine`, `graph`, `final`, `distance`).
- `tusk doctor` reports the graph-expansion pane.
- Workspaces without P2 (sub-units = false) still benefit from P3 over file-level embeddings, with the documented coarser-result tradeoff (§6.4 of the spec).

If any of these fails, P3 is not done.
