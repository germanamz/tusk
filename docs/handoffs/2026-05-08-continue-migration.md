---
type: handoff
title: Handoff 2026-05-08 — Continue dogfooding migration
session-date: "2026-05-08"
---

# Continue the tusk-on-tusk migration

## Where we left off

Three commits landed on `v1` in this session, bootstrapping the workspace:

- `fc679c2 chore(workspace): add dev pack and initialize tusk workspace`
- `e5ccfe4 chore(workspace): migrate docs to tusk nodes`
- `0a21096 chore(workspace): seed package status notes`

Workspace state at handoff time: 43 nodes (10 specs, 13 plans, 17 packages, 4 handoffs after this commit lands, 1 note), 53 edges. Structural indexing is fully active. Semantic indexing is still inert — that's the topic of `[[docs/handoffs/2026-05-08-install-ollama]]`, which should run before this one.

The design that drove the bootstrap is `[[docs/superpowers/specs/2026-05-08-tusk-workspace-bootstrap-design]]`.

## Outstanding migration follow-ups (do these in order)

### 1. Enable semantic indexing (depends on Ollama handoff)

After `[[docs/handoffs/2026-05-08-install-ollama]]` is done and `ollama serve` is healthy:

- Add `[embeddings] provider = "ollama" / endpoint / model / dim` to `tusk.toml` (exact block in the Ollama handoff).
- `./bin/tusk reindex` — queue populates, drains, embeddings table grows to ~43 rows.
- `./bin/tusk status` should show `embed queue depth: 0` and a populated `last reindex` timestamp.
- Smoke-test a semantic query via the MCP tool surface (`tusk_query` with `--semantic`) from a fresh Claude Code session to confirm the round-trip works.

### 2. Decide policy on body-wikilink dangling-edges

Doctor currently reports 16 informational `dangling-edge` warnings. Each is a wikilink-shaped mention in spec or plan PROSE — example wikilinks the writer used to illustrate something (think `notes/auth-rfc`, `tickets/foo`, etc., wrapped in double brackets). They aren't real cross-references; they're documentation of what wikilinks LOOK like.

The wikilink scanner (`internal/node/wikilinks.go`) skips triple-backtick fenced code blocks but NOT inline single-backtick code spans. So a double-bracketed example inside inline backticks in prose still materializes a dangling edge.

Three options:

- **A. Live with the noise.** Doctor is non-fatal; structural indexing works. Cheapest. Documented as "expected" in `docs/packages/reindex.md`.
- **B. Escape example wikilinks in existing docs.** Edit each spec / plan to wrap example wikilinks in fenced code blocks or use a non-bracket syntax (e.g., backslash escapes). Adds churn but cleans doctor output. Medium effort.
- **C. Enhance the scanner** to also strip inline-code spans (` `…` `). One small package change in `internal/node/wikilinks.go`; would need new tests. Cleanest fix; benefits any user of vault.

Recommendation: **C** as a small follow-up plan (or as a sub-bundle of Plan 8 if the wikilink scanner ends up being touched anyway). **A** is fine in the meantime.

### 3. Fix reindex's two-pass requirement

When a plan's `implements:` ref points at a spec by bare title and both are in the same reindex pass, the plan is processed before the spec (alphabetical walk: `plans/` < `specs/`). The title lookup fails on pass 1, leaving stale `ref_dangling` drift. Pass 2 succeeds because the spec is already in the DB. Documented in `[[docs/packages/reindex]]`.

Two ways to address:

- **Topologically order the walk** by node type (specs first, then plans, then handoffs / packages that reference them). Requires reading the manifest ref graph before walking.
- **Two-pass internally**: first pass parses + upserts nodes; second pass resolves refs against the now-complete table. Simpler change, slightly more expensive.

Either way, this is a `internal/reindex` change with new tests. Worth its own small plan, or roll into Plan 8.

### 4. Stale binary cleanup

`/workspaces/tusk/tusk` (~18 MB, Untracked, May 7 timestamp) is a stale build artifact from before `bin/tusk` became the canonical build target. The `Makefile` builds to `bin/tusk` now and `.gitignore` covers `bin/`. The root-level binary is just clutter — `rm /workspaces/tusk/tusk` is safe.

Either delete it ad-hoc or add `tusk` to `.gitignore` if there's a reason to keep building to root. (There isn't — every script and `.mcp.json` already targets `bin/tusk`.)

### 5. (Optional) Update existing `[[docs/handoffs/2026-05-08-plan-8-next]]`

That handoff predates this migration and references "re-reading static markdown" implicitly. Once the migration is humming, consider amending it to mention "query the workspace via MCP first" — or simply trust this handoff to do that.

## After this handoff

Plan 8. Scope is still open and the candidates are listed in `[[docs/handoffs/2026-05-08-plan-8-next]]`. Lead candidate per that handoff is §11.2 type-pack ergonomic shortcuts, but it's a brainstorm-first decision.

## References

- `[[docs/handoffs/2026-05-08-install-ollama]]` — pre-requisite for step 1
- `[[docs/handoffs/2026-05-08-plan-8-next]]` — the existing Plan-8 brainstorm prompt
- `[[docs/superpowers/specs/2026-05-08-tusk-workspace-bootstrap-design]]` — the design that drove the bootstrap
- `[[docs/packages/reindex]]` — reindex pipeline state and known issues
- `[[docs/packages/embed]]` — embed package, the on-ramp for semantic indexing
- `[[docs/packages/node]]` — wikilink scanner lives here
