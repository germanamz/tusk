# Phase 3 — Documentation & Roadmap Tick

## Goal

Ship the user-facing documentation that describes the new walk-up resolution
and workspace writes, and tick every story checkbox in the Local Config
Discovery initiative on `ROADMAP.md`. This is a narrow cleanup phase — no code
changes, no tests.

## Prerequisites

- **Phase 1 complete.** Walk-up in the resolver, `WithStartDir`, conditional
  global auto-create, relative `storage.path` resolution, walk-up e2e
  coverage.
- **Phase 2 complete.** `config set --global`, `config init --local`, and the
  "no file" error message in place with e2e coverage.
- No dependency on later initiatives.

## Inherits From

State after Phase 2:

- Running `tusk` from any subdirectory of a project with a `tusk.toml` walks
  up to find it and loads it. `Sources.File` is populated accordingly.
- Global `~/.config/tusk/config.toml` is no longer auto-created when a walk-up
  hit exists.
- `tusk config set` writes to the walk-up hit by default, the global file
  with `--global`, and errors helpfully when neither is available.
- `tusk config init --local` creates `./tusk.toml` from the current effective
  config; refuses to overwrite.
- All e2e scenarios under `tests/e2e/config_walkup_test.go` pass.
- No user-facing documentation has been updated yet — that is this phase's
  responsibility.

## Context Pointers

- `docs/configuration.md` — current configuration reference. The implementer
  will edit this file.
- `PRODUCT.md:354–425` — the canonical Configuration and Workspace Scope
  sections. Already describes walk-up; use it as the source of truth for
  wording. Do not re-edit unless a claim in `PRODUCT.md` is now wrong — it
  should all be accurate, since Phases 1 and 2 implemented exactly what it
  described.
- `ROADMAP.md:613–647` — the Local Config Discovery initiative with six
  unchecked stories. This phase ticks every `[ ]` under this initiative to
  `[x]`.
- `docs/status/` — if a v0.2 or later status doc exists for the current
  release, check whether it needs a line item. If no such file exists, skip.
- `README.md` — check for a short configuration section. If one exists and
  describes the old "single global config file" model, update it.

## Tasks

### Task 1 — Update `docs/configuration.md`

**File:** `docs/configuration.md`

Edit the existing configuration reference to cover the new behavior.
Required additions:

1. **Precedence section.** Replace the existing resolution order (if any)
   with the new chain:

   1. `--config <path>` flag — hard error when the file is missing.
   2. `TUSK_CONFIG` env var — hard error when the file is missing.
   3. Walk-up from the current directory toward the filesystem root,
      stopping at the first `tusk.toml` found. No symlink resolution.
   4. Global `~/.config/tusk/config.toml` (or the `TUSK_CONFIG_DIR` override).
      Auto-created on first run **only** when steps 1–3 all miss.
   5. Embedded defaults.

   Note in passing that individual `TUSK_*` environment variables still
   override values loaded from whichever file won — that layering is
   unchanged from the previous initiative.

2. **Walk-up walkthrough.** One short worked example:

   ```
   /home/you/work/acme/tusk.toml       <- found
   /home/you/work/acme/services/api/   <- CWD
   ```

   Running `tusk list` from `services/api/` uses the `tusk.toml` at
   `/home/you/work/acme/`. All relative paths inside that file — most
   importantly `storage.path` — resolve against `/home/you/work/acme/`,
   not the caller's CWD.

3. **Workspace-scoped writes.** Document the new `config set` behavior:

   - Default: writes to whichever file `config show` reports as active.
   - `--global`: writes to the global file regardless of walk-up.
   - With no local file and no `--global`: returns the "no config file
     found" error and suggests `tusk config init` or
     `tusk config init --local`.

4. **`config init --local`.** Document the command:

   ```bash
   tusk config init --local   # writes ./tusk.toml with current effective config
   tusk config init           # writes the global file (unchanged)
   ```

   Note that `init --local` refuses to overwrite an existing file.

5. **Conditional global auto-create.** One sentence explaining that
   `~/.config/tusk/config.toml` is only spawned on first run when the walk-up
   finds nothing — running tusk inside a project with its own `tusk.toml`
   never creates a global file.

6. **Workspace scope.** If `docs/configuration.md` does not already cover the
   "one database per config file" model, port the relevant paragraph from
   `PRODUCT.md:416–424`. Do not duplicate verbatim — paraphrase so the two
   documents reinforce each other without drifting.

Do not introduce new sections that the surrounding docs cannot support. If
`docs/configuration.md` currently has a flat structure, match it. If it uses
headings, add headings.

### Task 2 — Tick the Local Config Discovery stories in `ROADMAP.md`

**File:** `ROADMAP.md`

Flip every `[ ]` to `[x]` under the `### Initiative: Local Config Discovery`
heading (lines 613–647). Specifically, the following six story headers and
all bullets under them:

- `Story: Walk-up step in the resolver`
- `Story: Relative paths resolve to the config file's directory`
- `Story: Workspace-aware config set`
- `Story: config init --local`
- `Story: Conditional global auto-create`
- `Story: config show / config path report walk-up hits`

Before ticking each bullet, read it and verify the code actually satisfies
the claim. If any bullet is not satisfied, **do not tick it** and instead
flag the gap in the commit message so the planning agent can revisit.
Expected outcome: all bullets tick cleanly because Phases 1 and 2 were
specified to cover them.

Do not touch any other initiative in `ROADMAP.md`. Do not reorder sections.
Do not rewrite the initiative summary paragraph.

## User-Visible Behaviors To Preserve

Documentation changes have no runtime impact, but preserve these invariants
while editing:

- `docs/configuration.md` must stay internally consistent — do not leave
  references to behaviors that were removed (e.g. "the global config file is
  always created on first run" without the walk-up qualifier).
- `ROADMAP.md` structure stays intact. Only the six checkboxes flip.
- `PRODUCT.md` is not edited unless a specific claim is wrong. (It should
  not be.)

## Changes Introduced

**Modified files**

- `docs/configuration.md` — walk-up precedence, workspace-scoped writes,
  `--global` flag, `config init --local`, conditional global auto-create.
- `ROADMAP.md` — six stories ticked under Local Config Discovery.
- Optional: `README.md` — only if it has a stale configuration blurb.

**No code changes. No tests. No new files.**

**No schema migrations. No new dependencies. No new environment variables.**

**Bridge code:** none.

## Commit

One commit:
`docs: document local config discovery and tick roadmap stories`

Body should list the six ticked stories and note that `PRODUCT.md` already
described the target behavior so was not re-edited.
