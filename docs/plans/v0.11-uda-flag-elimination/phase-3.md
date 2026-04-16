# Phase 3 — Documentation sweep

**Milestone:** v0.11
**Initiative:** UDA Flag Elimination
**Phase:** 3 of 3
**Prerequisites:** Phase 2 must be complete.
**Parallelizable:** No — Phase 2's behavioral changes must be landed so doc examples match reality.

## Inherits From

Phase 2 rewired `runCreate` and `runModify` to use `collectUDAs` and `validateKnownFields`, deleted `parseUDAFlags`, removed the `--uda` / `-u` flag from both commands, and added full E2E coverage for all happy and error paths. The inline `uda.key=value` syntax is now the only way to set UDAs from the CLI, and unknown top-level fields on create/modify are rejected with an error.

## Intent

Sweep all documentation to reflect the new UDA surface. Add a principle statement for dotted UDA fields in the inline syntax section of `PRODUCT.md`, update CLI examples, tick the ROADMAP stories, and confirm `docs/configuration.md` has no stale `--uda` references. This is intentionally narrow — pure markdown edits, no code changes.

## Tasks

### 1. Update `PRODUCT.md`

Open `PRODUCT.md` and make the following edits:

**CLI examples section** (under "### CLI"). Find the task creation and modification examples. Add UDA examples alongside the existing lines if not already present:

```bash
tusk task create "Deploy monitoring" project=ops +infra priority=3 uda.env=prod uda.region=eu
tusk task modify a3f8b2c1 uda.team=backend          # add/update a UDA
tusk task modify a3f8b2c1 uda.env=                   # delete a UDA key
```

Grep `PRODUCT.md` for `--uda`. If any references exist, replace them with the inline syntax. Expected: zero hits (the v0.11 command grouping and string-field initiatives already cleaned most, but sweep defensively).

**Inline Syntax section** (under "### Inline Syntax"). Locate the paragraph that describes field syntax. Add a one-sentence note at the end of the fields bullet or as its own bullet:

> `uda.key=value` on create and modify sets a user-defined attribute; an empty value on modify deletes the key. Bare `key=value` that is not a reserved field or a `uda.*` field is rejected as unknown — typos on UDA keys surface loudly instead of slipping through.

Do not restructure the section — add, do not rewrite.

**User-Defined Attributes section** (under "### User-Defined Attributes"). If the section says UDAs are set "via `--uda key=value`" or similar flag-based language, rewrite to say UDAs are set via inline `uda.key=value` syntax on `tusk task create` and `tusk task modify`. Mention filtering with `uda.key=value` in the same sentence to reinforce that the same syntax works everywhere.

### 2. Tick ROADMAP stories

Open `ROADMAP.md`. Under `## v0.11 — CLI Command Grouping`, locate the `### Initiative: UDA Flag Elimination` section. The three stories should be:

1. **Story: Dotted UDA field recognition in task commands** — check `[x]` on the story and all its sub-bullets.
2. **Story: Drop `--uda` / `-u` flag** — check `[x]` on the story and all its sub-bullets.
3. **Story: MCP parity check for UDAs** — check `[x]` on the story and all its sub-bullets.

Do not modify any other initiative or milestone. Do not add or delete stories.

### 3. Sweep `docs/configuration.md`

Open `docs/configuration.md`. Grep for `--uda`, `-u `, `uda flag`, and `parseUDA`. If any match is found, rewrite the relevant sentence(s) to reference the inline `uda.key=value` syntax. If zero matches are found (expected), this task is a no-op — confirm with a note in the commit message.

Do **not** create or modify `docs/releases/v0.11.md` or `docs/status/v0.11-status.md`. Those land at milestone completion, not per-initiative.

## Acceptance criteria

After Phase 3, every doc that a user or agent might consult (`PRODUCT.md`, `ROADMAP.md`, `docs/configuration.md`) consistently describes UDAs as set via inline `uda.key=value` syntax. No mention of `--uda` or `-u` remains in any shipped documentation. No code changes — `go test ./...` should still pass unmodified from Phase 2.

Specific checks:

- `grep -rn '\-\-uda' PRODUCT.md ROADMAP.md docs/` returns zero hits.
- `grep -rn '"-u "' PRODUCT.md ROADMAP.md docs/` returns zero hits.
- All three ROADMAP stories under UDA Flag Elimination are marked `[x]`.
- No new files created.

## Changes Introduced

**Modified files:**
- `PRODUCT.md` — CLI examples, inline syntax section, and UDA section updated.
- `ROADMAP.md` — three stories ticked under UDA Flag Elimination.
- `docs/configuration.md` — stale `--uda` references removed (if any existed).

**Not changed in this phase:**
- Any Go source file.
- Any file under `internal/mcp/`.
- `docs/releases/` or `docs/status/`.
