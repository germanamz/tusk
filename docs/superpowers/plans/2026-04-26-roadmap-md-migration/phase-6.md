# Phase 6 — Cutover plumbing and migration tracker scaffold

**Initiative:** ROADMAP.md Migration (v0.13)
**Spec:** `docs/superpowers/specs/2026-04-26-roadmap-md-migration-design.md`
**Prerequisites:** Phase 5.

## Inherits From

After Phase 5:

- `tusk task tree project=<name> --format markdown` is feature-complete: H1, project description, project-level notes, headings/bullets with full title-line tokens, descriptions, annotations, and notes.
- The renderer produces deterministic output for a given workspace state.
- `tests/e2e/tree_markdown_test.go` proves the CLI plumbing end-to-end.
- `make test` and `make test-race` are green.

## Goal

Land the contributor-facing tooling and content scaffolding that the user needs to:

1. Carry out the manual migration of `ROADMAP.md` into the `tusk-roadmap` project.
2. Run `make roadmap` to regenerate `ROADMAP.md` from tusk state once migration is complete.
3. Catch any future hand-edit of `ROADMAP.md` via CI.

This phase **does not** carry out the manual migration itself — that is a user-driven workflow that happens after Phase 6 lands. This phase ships only the tooling and the empty tracker doc.

This phase is intentionally narrow (4 tasks) — it is glue and content rather than code, and merging it with Phase 5 would dilute the renderer focus.

## Tasks

### Task 1 — `make roadmap` target

In the existing `Makefile`, add a `roadmap` target. Match the indentation/format of existing targets (`build`, `test`, `test-race`, `vet`, `lint`).

Target body:

```makefile
.PHONY: roadmap
roadmap: build
	./bin/tusk task tree project=tusk-roadmap --format markdown > ROADMAP.md
```

Notes:

- `roadmap` depends on `build` so the produced binary is always current.
- It writes via the *built binary* explicitly (`./bin/tusk`), not via `go run`, to match the build-artifact contract used elsewhere in the Makefile.
- Use `>` (overwrite). The user's source of truth is tusk; regenerating must always replace.
- The target is named `roadmap` (singular) to read naturally as `make roadmap`.

If the existing Makefile uses tabs for indentation (it must — Make requires it), preserve that. Verify the file edits cleanly with `make -n roadmap`.

### Task 2 — CI drift check

Add a CI step that fails if `ROADMAP.md` and tusk state disagree.

1. Inspect `.github/workflows/` for the existing CI workflow (likely `ci.yml` or similar). Identify the job that runs `make test` / `make lint`.
2. Add a new job (or step in the existing job — pick whichever matches the convention used for `lint`, `vet`, etc.) named `roadmap-drift`:
   ```yaml
   - name: Verify ROADMAP.md is up to date
     run: |
       make roadmap
       git diff --exit-code ROADMAP.md
   ```
3. The step assumes the `tusk-roadmap` project exists in whatever workspace CI runs against. **Until the manual migration completes, the project does not exist and the step would fail.** Gate the step:
   ```yaml
   - name: Verify ROADMAP.md is up to date
     if: ${{ vars.TUSK_ROADMAP_CHECK_ENABLED == 'true' }}
     run: |
       make roadmap
       git diff --exit-code ROADMAP.md
   ```
   Document the env-var/repo-variable gate in `CONTRIBUTING.md` (Task 3): set `TUSK_ROADMAP_CHECK_ENABLED=true` once the cutover commit lands. Until then, the step is a no-op on every PR.

If the CI workflow runs against a clean checkout (no pre-existing `tusk.db`), the step also needs the workspace to exist. The simplest guard is the variable check above — by the time `TUSK_ROADMAP_CHECK_ENABLED=true` is set, the cutover commit has populated the repo with a `tusk.db` (or seed/import process) that gives the CI runner the project. Note in `CONTRIBUTING.md` that this gate is the user's lever.

**Important:** do not modify the existing CI job's success criteria. The new step / job is gated and must default to a green state.

### Task 3 — Contributor docs update

Update `CONTRIBUTING.md` (or the file the repo uses for contributor onboarding — search the repo root for `CONTRIBUTING*` and `README*`; if there is no `CONTRIBUTING.md`, append the section to `README.md` under a new `## Roadmap maintenance` heading).

Add a section titled `## ROADMAP.md is generated`:

```markdown
## ROADMAP.md is generated

`ROADMAP.md` is regenerated from tusk state — do not hand-edit.

Workflow:

1. Edit the roadmap via `tusk task` commands. Examples:
   ```bash
   tusk task create "Story: my new story" level=story project=tusk-roadmap parent=<initiative-short-id>
   tusk task done <short-id>
   tusk task move <short-id> --before <target>
   ```
2. Run `make roadmap` to regenerate `ROADMAP.md`.
3. Commit the regenerated `ROADMAP.md` alongside whatever code change motivates the roadmap edit.

CI will fail any PR whose `ROADMAP.md` is out of sync with tusk state once the
gate is enabled (`TUSK_ROADMAP_CHECK_ENABLED=true` repo variable). The variable
flips on at the cutover commit (post-manual-migration); until then the check is
a no-op.

The `tusk-roadmap` project uses the canonical taxonomy:
`milestone:initiative:story:(task,spike)`.
```

Tone: instructive, terse, matches the rest of the file.

### Task 4 — Migration tracker scaffold

Create `docs/status/v0.13-roadmap-migration.md` with an empty checklist mirroring the structure of the source `ROADMAP.md` at the time of this commit. The file is a working document — the user will tick boxes as they migrate each item.

Generate the structure by reading `ROADMAP.md` and converting:

- Each `## v0.X — Name` (milestone) → top-level `- [ ] Milestone: v0.X — Name` bullet.
- Each `### Initiative: Name` → `  - [ ] Initiative: Name` sub-bullet.
- Each `- [x|space] **Story: Name**` → `    - [ ] Story: Name` (always unchecked in the tracker — the tracker tracks whether the *migration* of that item is done, not the underlying status).
- Each `  - [x|space] sub-bullet` → `      - [ ] sub-bullet text`.

Preserve document order. Drop the `**Goal:**` lines, `> blockquote` summaries, `> **Deferred:**` blocks, and other prose — the tracker mirrors only the migratable items, since the prose lives in `description=` fields the user creates manually.

Top of the file:

```markdown
# v0.13 — ROADMAP.md Migration Tracker

Tracks manual migration of `ROADMAP.md` into the `tusk-roadmap` project.
Once every box is checked and `tusk task tree project=tusk-roadmap --format markdown` matches the source, this file (and the original `ROADMAP.md`) get
replaced by the regenerated output. Removed at cutover.

Taxonomy: `milestone:initiative:story:(task,spike)`.

Project setup (one-time, before migration starts):

```bash
tusk project create tusk-roadmap workflow=kanban description=@./tmp/vision.md
tusk project modify tusk-roadmap taxonomy.levels=milestone:initiative:story:(task,spike)
```

Workflow:

1. For each unchecked item: `tusk task create "..." level=... project=tusk-roadmap parent=<...>` (with `description=@./tmp.md` when the source carries prose).
2. If the source bullet is `[x]`, follow with `tusk task done <short-id>`.
3. Tick the box here, append `→ <short-id>`.

## Tracker

<!-- generated from ROADMAP.md at <commit-sha>; structure mirrors source -->

[ ... generated checklist ... ]
```

The implementer agent must produce the full checklist by walking the current `ROADMAP.md` and emitting one line per migratable item. **Do not** attempt to parse prose; only extract heading and bullet structure. If the source contains a heading or bullet whose text could be interpreted multiple ways (e.g., a deferred-feature blockquote), treat it as prose and skip it — it will be folded into the parent item's `description=` during manual migration.

The tracker can be rebuilt later if the source moves; for now this commit captures the snapshot.

## User-visible behaviors (acceptance criteria)

- All Phase 5 acceptance criteria still hold. No phase 6 task touches renderer code; the renderer-related behaviors must remain intact (verified by re-running `make test` / `make test-race`).
- `make roadmap` succeeds against a workspace where the `tusk-roadmap` project exists with at least one task. Against an empty or missing project it produces an error message that points the user at the manual migration workflow (the underlying `tusk task tree` returns a non-zero exit when the project does not exist; the Makefile target propagates that exit code).
- `make roadmap` overwrites `ROADMAP.md` deterministically — running it twice with no state change produces a byte-identical file.
- The CI step `Verify ROADMAP.md is up to date` is registered in the workflow file but gated behind the `TUSK_ROADMAP_CHECK_ENABLED` repo variable. The default-off gate keeps every PR green until the cutover commit flips it on.
- `CONTRIBUTING.md` (or chosen contributor doc) carries the "ROADMAP.md is generated" section.
- `docs/status/v0.13-roadmap-migration.md` exists with an unchecked checklist mirroring the structure of `ROADMAP.md` at the time of commit.
- `make test` and `make test-race` pass (no test code changes, but the new files must not break the build).
- The existing `ROADMAP.md` is **not** modified by this phase. The user-driven manual migration replaces it post-Phase-6.

## Bridge code introduced

None.

The CI gate (`TUSK_ROADMAP_CHECK_ENABLED`) is technically a feature flag, but it is not bridge code — it is a permanent operational lever the user controls, not a hook the implementer must remove. Tag it in `CONTRIBUTING.md` so the cutover step (post-Phase-6) knows to flip it.

## Changes Introduced

- **New files:**
  - `docs/status/v0.13-roadmap-migration.md` (migration tracker scaffold)
- **Modified files:**
  - `Makefile` (new `roadmap` target)
  - `.github/workflows/<existing CI workflow>` (new gated step)
  - `CONTRIBUTING.md` or `README.md` (new "ROADMAP.md is generated" section)
- **No schema migrations.**
- **No new dependencies.**
- **No new environment variables consumed by tusk itself.** The CI gate (`TUSK_ROADMAP_CHECK_ENABLED`) is a CI-only repo variable.
- **No public API additions.**

## What happens after this phase

Out of scope for any implementer agent — these are user-driven steps:

1. The user creates the `tusk-roadmap` project and runs the manual migration over some number of sessions, ticking the tracker.
2. Once every box is ticked, the user runs `make roadmap`, replaces `ROADMAP.md` with the regenerated output, deletes `docs/status/v0.13-roadmap-migration.md`, and flips `TUSK_ROADMAP_CHECK_ENABLED=true` on the repo.
3. That commit is the cutover.

The post-implementation review (run by the planning agent after all six phases ship) confirms the tooling works end-to-end against a small fixture and signs off the initiative.
