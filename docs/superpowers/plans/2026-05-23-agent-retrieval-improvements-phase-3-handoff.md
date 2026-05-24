# Phase 3 Handoff — Graph-Aware Semantic Ranking

You are the implementer for **Phase 3** of the agent-retrieval-improvements initiative. Phase 1 must be merged before you start. Phase 2 is recommended but not strictly required — the plan supports both scenarios.

## Working Documents

- **Plan (your primary directive):** `docs/superpowers/plans/2026-05-23-agent-retrieval-improvements-phase-3.md`
- **Design spec (rationale and context):** `docs/superpowers/specs/2026-05-23-agent-retrieval-improvements-design.md`
- **Inherited from prior phases (already on main):** `…-phase-1.md`, `…-phase-2.md`

The plan is the authoritative source for what to do. The spec explains why. The prior phase plans document the interfaces you inherit.

## Prerequisites

- **Phase 1 merged to `main`.** Verify the post-P1 files exist (`internal/cliregistry`, `internal/aliasdispatch`, etc.).
- **Phase 2 is optional but strongly recommended.** P3 works either way, but produces materially better results when sub-units are present. If P2 is also merged, verify the post-P2 files exist (`internal/subunit/`, `internal/typepacks/subdocument/`) and that `[workspace] sub-units = true` is the default.
- `make test`, `make vet`, `make lint` all green on `main`.
- Ollama running and reachable for the integration tests that exercise actual semantic ranking.

## Branch and Workspace

- Create a feature branch from `main`. Name it `feat/agent-retrieval-p3`.
- **Work in-place in the current checkout.** No git worktree.
- Commit per task. Pre-commit runs the full test suite.

## Required Sub-Skill

Use `superpowers:subagent-driven-development`. One subagent per task with review between tasks.

## Tasks (Four Total)

1. Manifest configuration block and per-call override flags.
2. Graph-walk implementation.
3. Re-ranking and blending into the query path.
4. Doctor surfacing and integration verification.

## Acceptance Criteria

The plan's final section, "User-Visible Behaviors That Must Still Work," is your acceptance checklist. Specifically:

- All P1 (and P2, if applied) user-visible behaviors continue to work.
- A workspace without `[query.graph-expansion]` produces identical semantic-query results to pre-P3 — the feature is fully default-off.
- Enabling `[query.graph-expansion] enabled = true weight = 0.2` shifts the result set in the direction the spec describes (graph-proximate nodes surfacing alongside word-similar ones).
- `tusk query --semantic "..." --graph-expand` enables for one call without manifest changes.
- `tusk query --semantic "..." --explain` shows the per-result breakdown (`cosine`, `graph`, `final`, `distance`).
- `tusk doctor` reports the graph-expansion pane, including warnings for unknown edge types and weight=0 no-ops.
- The central P3 acceptance test (Task 3): a fixture vault where graph-expansion surfaces a node that pure cosine missed.

## Critical Pitfalls (Reread Before Starting Each Task)

- **Cosine score range.** Some embedding models produce [-1, 1] cosines instead of [0, 1]. Verify nomic-embed-text's range early; clip or shift if needed so blended scores stay in [0, 1].
- **Don't double-count edges.** When two seeds share an edge, dedupe at the walker's output, not at the blender.
- **Default precedence.** `--no-graph-expand` must beat workspace `enabled = true`. Use a tri-state (nil / true / false) on the CLI flag.
- **Performance discipline.** §6.5 of the spec sets the cost ceiling: K × avg_degree edge lookups per hop, single SQL with `IN`. Don't loop per-candidate; batch every walk.
- **The `--explain` breakdown is essential.** Don't skip it; it's the only debugging tool the user has when graph expansion produces a surprising ranking.

## What to Commit

- One commit per task. Conventional Commits format.
- Commit regenerated `docs/cli/` and `man/` after Task 1 (new CLI flags on `tusk query`) and Task 4 (doctor output changes).
- Do **not** commit the spec, plans, or handoffs themselves.
- Do **not** add AI-attribution trailers.

## If You Get Stuck

- **Plan ambiguity:** consult the spec section the plan references.
- **GraphRAG context:** Microsoft Research's 2024 paper is the conceptual reference; the spec cites it in §10. Our implementation is the simplest form of the pattern.
- **Existing semantic-query flow:** `internal/mcp/tools.go` (the `tusk_query` handler) post-P1 lives in the service extracted in P1 Task 1; the cosine ranking step is the integration point for the walker.
- **Existing edge lookups:** `internal/index/edge_repo.go` for the batched lookup pattern.

## When You're Done

- Push the branch.
- Open a PR titled `feat: agent retrieval improvements (Phase 3)`.
- Hand the PR URL back to the planning agent for post-implementation review.
- Do **not** merge until the planning agent has approved.

After P3 is merged, the planning agent runs the final sequence verification across all three phases per `phase-post-implementation-review`. At that point the plan and handoff documents are cleaned up from the repository.
