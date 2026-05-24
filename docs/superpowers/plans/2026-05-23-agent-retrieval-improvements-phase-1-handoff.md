# Phase 1 Handoff — Rich Query + Memory Entry Point

You are the implementer for **Phase 1** of the agent-retrieval-improvements initiative. This document is your starting brief. Read it once, then execute against the plan.

## Working Documents

- **Plan (your primary directive):** `docs/superpowers/plans/2026-05-23-agent-retrieval-improvements-phase-1.md`
- **Design spec (rationale and context):** `docs/superpowers/specs/2026-05-23-agent-retrieval-improvements-design.md`
- **Future phases (reference only — do not implement):** `docs/superpowers/plans/2026-05-23-agent-retrieval-improvements-phase-2.md`, `…-phase-3.md`

The plan is the authoritative source for what to do and in what order. The spec exists to explain why; consult it when the plan's intent is unclear. Future phase docs are visible so you can verify your bridge points line up, but you do not implement them.

## Prerequisites

- Starting from current `main`. No earlier phases.
- All `make test`, `make vet`, `make lint` green on `main` before you start. Verify with `make test && make vet && make lint`.
- Ollama is not required for P1 (semantic features stay unchanged; embedding behavior is untouched).

## Branch and Workspace

- Create a feature branch from `main`. Name it `feat/agent-retrieval-p1` (matches the user's branch-naming convention: `<type>/<kebab-case>`, enforced by lefthook pre-push).
- **Work in-place in the current checkout.** Do not spawn a git worktree (per user preference).
- Commit per task as the plan directs. Lefthook pre-commit runs the full Go test suite — never leave tests red between commits.

## Required Sub-Skill

Use `superpowers:subagent-driven-development` to execute the plan. The plan is structured for one subagent dispatch per task with two-stage review between tasks.

## Tasks (Five Total)

1. Service-layer extraction and positional-name registry.
2. Filter grammar — `modified-since:` predicate.
3. `include`, `fields`, `format` on read tools, plus compact renderer.
4. Alias mechanism (`[alias.<name>]`, `tusk run`, `tusk_run`).
5. `tusk_context` — the warm-context entry point.

Each task in the plan has its own checklist of steps. Mark steps complete with `[x]`. Do not skip the verification steps at the end of each task.

## Acceptance Criteria

The plan's final section, "User-Visible Behaviors That Must Still Work," is your acceptance checklist. Confirm every bullet works before declaring P1 done. Specifically:

- All existing CLI and MCP behavior is unchanged for callers that don't use new flags.
- `tusk node list type=ticket --include body,edges` returns ticket bodies and edges inline.
- `tusk run <alias>` and `tusk_run` work against manifest-declared aliases.
- `tusk context` and `tusk_context` return the composed digest.
- `tusk doctor` reports invalid aliases and invalid `[context]` blocks.
- `modified-since:7d type=note` is a valid filter expression.

## What to Commit

- One commit per task. Commit messages follow the Conventional Commits format used elsewhere in the repo: `feat(...)`, `refactor(...)`, `docs(...)`, `chore(...)`.
- After Task 2 (filter grammar) and Task 3 (CLI flag changes), run `make docs` and include the regenerated `docs/cli/` and `man/` artifacts in the same commit — the pre-push docs-drift hook rejects pushes otherwise.
- Do **not** auto-commit the spec or plan documents themselves; leave them untracked unless the user explicitly asks.
- Do **not** add AI-attribution trailers to commits.

## If You Get Stuck

- **Plan ambiguity:** consult the spec section the plan references.
- **Code patterns:** read `docs/packages/*.md` for the package you're touching; these are the authoritative summaries of public surface and intent.
- **Existing service shape:** the MCP handlers in `internal/mcp/tools.go` are the closest existing example of "tool → service call" — model the refactor after that pattern.

## When You're Done

- Push the branch.
- Open a PR titled `feat: agent retrieval improvements (Phase 1)`.
- Hand the PR URL back to the planning agent for post-implementation review.
- Do **not** merge until the planning agent has run `phase-post-implementation-review`.
