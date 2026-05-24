# Phase 2 Handoff — Sub-Document AST

You are the implementer for **Phase 2** of the agent-retrieval-improvements initiative. Phase 1 must be fully merged before you start.

## Working Documents

- **Plan (your primary directive):** `docs/superpowers/plans/2026-05-23-agent-retrieval-improvements-phase-2.md`
- **Design spec (rationale and context):** `docs/superpowers/specs/2026-05-23-agent-retrieval-improvements-design.md`
- **Inherited from Phase 1 (already on main):** `docs/superpowers/plans/2026-05-23-agent-retrieval-improvements-phase-1.md`
- **Future phase (reference only — do not implement):** `docs/superpowers/plans/2026-05-23-agent-retrieval-improvements-phase-3.md`

The plan is the authoritative source for what to do. The spec explains why. The Phase 1 plan documents the interfaces you inherit; consult its "Changes Introduced" section if you need to know what shape a service function or struct has.

## Prerequisites

- **Phase 1 merged to `main`.** Verify by inspecting `git log --oneline` for the P1 commits, and confirming the existence of the P1-introduced files (e.g., `internal/cliregistry/registry.go`, `internal/aliasdispatch/dispatch.go`, `internal/contextcompose/compose.go`).
- `make test`, `make vet`, `make lint` all green on the post-P1 `main`.
- Ollama running and reachable on `localhost:11434` for the embedding integration tests (set `OLLAMA_NUM_PARALLEL=4` for parallel embed throughput on this host — see the user's environment notes; re-set after a reboot).
- Confirm goldmark is not already pulled in by checking `go.mod`. If it is, note the version; if it isn't, the plan adds it in Task 2.

## Branch and Workspace

- Create a feature branch from `main` (post-P1). Name it `feat/agent-retrieval-p2`.
- **Work in-place in the current checkout.** No git worktree.
- Commit per task. Pre-commit runs the full test suite; the **schema migration in Task 1 changes table structure** — run a smoke test against a fixture workspace to confirm migration idempotency before committing Task 1.

## Required Sub-Skill

Use `superpowers:subagent-driven-development`. One subagent per task with review between tasks. Task 1 (schema migration) is the highest-risk task in P2 — review its diff carefully before allowing Task 2 to proceed.

## Tasks (Six Total)

1. Schema migration and `sub-document` built-in type pack.
2. Markdown AST parser and content-hash identity.
3. Reindex pipeline integration — hash diff, insert/delete, edge re-derivation.
4. AST-driven chunker replacing `MarkdownRecursive` for sub-unit workspaces.
5. `tusk_query` surfacing — `matched_units`, heading-level weighting, `include = units`.
6. Doctor sub-unit pane and final integration verification.

## Acceptance Criteria

The plan's final section, "User-Visible Behaviors That Must Still Work," is your acceptance checklist. Specifically:

- All P1 user-visible behaviors continue to work.
- A workspace with `sub-units = false` behaves identically to current `main` (back-compat path).
- A workspace with `sub-units = true` (default) reindexes successfully and produces sub-unit rows.
- Editing one paragraph replaces only that paragraph's row in the index (the "natural rebalancing" invariant from §5.2 of the spec).
- `tusk query type=list-item checkbox=false` returns open todos across the vault.
- `tusk query --semantic` returns parent files with `matched_units` attached, including `section` rows with `heading_level`.
- Wikilinks inside paragraph bodies materialize edges from the paragraph to the target file (never to a sub-unit).
- `tusk doctor` reports the sub-unit pane.

## Critical Pitfalls (Reread Before Starting Each Task)

- **Hash inputs must use normalized text** (line-endings, whitespace). Otherwise the same content produces different hashes on different platforms, invalidating every workspace's index on upgrade.
- **The PK migration on `embeddings` is one-way.** Test on a fixture with thousands of rows before committing Task 1.
- **Foreign keys are off by default in SQLite.** Ensure `PRAGMA foreign_keys = ON` runs at every `Open`; the `ON DELETE CASCADE` behavior P2 depends on requires this.
- **Goldmark normalizes whitespace.** Hash inputs must use goldmark's normalized form, not raw bytes.
- **`tusk_query` semantic ranking changes shape under sub-units.** Add a regression test that exercises `sub-units = false` to verify the back-compat path remains identical to current `main`.
- **`make docs` after Task 5 and Task 6.** The pre-push docs-drift hook rejects pushes that change CLI help text without regenerated man pages.

## What to Commit

- One commit per task. Conventional Commits format.
- Commit the goldmark dependency change with Task 2 (the task that first uses it).
- Commit regenerated `docs/cli/` and `man/` after Task 5 (filter grammar gains new `type=` values via the type pack) and Task 6 (doctor output changes).
- Do **not** commit the spec, plans, or handoffs themselves.
- Do **not** add AI-attribution trailers.

## If You Get Stuck

- **Plan ambiguity:** consult the spec section the plan references.
- **Goldmark API questions:** the library has thorough godoc; for the AST visitor pattern, see goldmark's `ast.Walk` signature.
- **Existing reindex flow:** `docs/packages/reindex.md` summarizes the pipeline; `internal/reindex/reindex.go` is the entry point.
- **Existing chunker behavior:** `docs/packages/embed.md` plus `internal/embed/chunking.go`.

## When You're Done

- Push the branch.
- Open a PR titled `feat: agent retrieval improvements (Phase 2)`.
- Hand the PR URL back to the planning agent for post-implementation review.
- Do **not** merge until the planning agent has approved.
