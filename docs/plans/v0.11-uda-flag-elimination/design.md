# UDA Flag Elimination — Design

**Milestone:** v0.11
**Initiative:** UDA Flag Elimination
**Status:** Draft
**Last updated:** 2026-04-15

## Goal

Drop `--uda` / `-u` from `tusk task create` and `tusk task modify`. UDAs flow through the inline `key=value` syntax as dotted fields (`uda.env=prod`), matching the filter syntax that already accepts them and closing the last flag-based gap in the v0.11 inline-field principle. As a precondition, add strict unknown-top-level-field rejection on create/modify so dotted UDA keys are the only way to reach the UDA map, and typos surface loudly instead of slipping through.

## Scope

Ships:

1. A `tui.collectUDAs(fs)` helper walking `fs.Fields`, splitting `uda.<key>`, validating via `domain.ValidateUDAKey`, returning `map[string]any`.
2. A `tui.validateKnownFields(fs)` helper rejecting any field whose key is not in the reserved allowlist and does not start with `uda.`.
3. `runCreate` / `runModify` rewired to call both helpers; `--uda` / `-u` Cobra flag removed from both commands.
4. Modifier rejection: any `FieldFilter.Modifier != 0` on a `uda.*` field errors with "modifier not supported on uda fields".
5. Deletion of `parseUDAFlags` and the `uda_test.go` cases targeting it, replaced by tests for the new helpers.
6. E2E coverage for create, repeated-field create, modify set, modify delete (empty value), invalid UDA key, unknown top-level field (with and without a `did-you-mean` hint), and stale `--uda` / `-u` flag invocations.
7. Doc sweep: `PRODUCT.md` CLI examples and the inline-syntax principle paragraph; `ROADMAP.md` story ticks; `docs/configuration.md` if it mentions `--uda`.

Out of scope:

- MCP code changes. Tool schemas already accept `uda` as a structured JSON object — verification grep only, no files under `internal/mcp/` are modified.
- Per-project UDA schema validation. That lives in v0.14.
- Lexer or AST changes. The v0.9 lexer already accepts dotted keys — filter resolution has used `uda.<key>` on the same `FieldFilter` shape since v0.5.
- `docs/releases/v0.11.md` and `docs/status/v0.11-status.md`. Those land at milestone completion, not per-initiative (per the status-files-milestone-only memory).

## Semantics

### Dotted key recognition

A field is a UDA iff `strings.HasPrefix(f.Key, "uda.")`. The tail (`f.Key[4:]`) is the UDA key. The tail runs through `domain.ValidateUDAKey`, so:

- `uda.env=prod` → tail `env`, valid.
- `uda.=x` → empty tail, rejected with the existing "UDA key must not be empty" message.
- `uda.1env=x` → tail starts with digit, rejected by the domain regex.
- `uda.a.b=x` → embedded dot in tail, rejected by the domain regex.

No new validation rules — the pipeline leans on `ValidateUDAKey` for every legitimacy check.

### Create semantics (`runCreate`)

- Collect every `uda.*` field into `map[string]any`. Duplicates (`uda.env=a uda.env=b`) resolve last-wins, matching the StringArray semantics of the old `--uda` flag.
- Empty value (`uda.env=`) stores the empty string. Identical to today's `--uda env=` behavior. No special-casing — the merge-delete semantics only apply on modify.
- The map is assigned via `task.UDA = m` only when at least one `uda.*` field is present. Absent → `task.UDA == nil` and the service path is unchanged.

### Modify semantics (`runModify`)

- Same collection pass.
- `upd.UDA = &m` only when at least one `uda.*` field is present. Absent → the outer pointer stays nil and the service leaves existing UDAs untouched.
- Empty value (`uda.env=`) passes `{"env": ""}` to the service, which already interprets an empty-string value as a key deletion (`TestUpdate_UDAMerge_DeleteKey`).
- There is no way through the CLI to set a UDA value to the literal empty string. This is a pre-existing asymmetry between create and modify, and it is preserved — documented here as a known limit, not fixed in this initiative.

### Modifier rejection

If any `uda.*` field carries `Modifier != 0`, the helper returns `fmt.Errorf("modifier %q not supported on uda fields", string(f.Modifier))` before building the map. Applies uniformly to create and modify. The rationale: empty-value delete on modify already handles the only real use case (`uda.env=`), and rejecting `+`/`-` prefixes keeps the surface narrow — they can be added later if a concrete need appears.

### Unknown top-level field rejection

The reserved set for `tusk task create` and `tusk task modify` is:

```
title, description, project, priority, status, due, parent
```

After `collectUDAs` runs, `validateKnownFields` walks `fs.Fields` again: for each field whose key is not in the reserved set and does not begin with `uda.`, it returns an error.

- Bare unknown (key with no `.`): `fmt.Errorf("unknown field %q; did you mean uda.%s?", key, key)`. The hint fires on the typo case — a user meant to attach a UDA but forgot the prefix.
- Dotted unknown (key contains `.` but not a `uda.` prefix): `fmt.Errorf("unknown field %q", key)`. No hint — a dotted key is clearly intentional, a `did-you-mean` would be noise.

`tree` is a filter-only field and is not in the reserved set. Currently `runCreate` and `runModify` silently ignore it; after this initiative a `tree=` argument errors as unknown. That is the intended behavior.

Tag fields (`+tag` / `-tag`) parse as `TagFilter`, not `FieldFilter`, so the unknown-field walk never sees them. Tags continue to flow through their existing path.

### Ordering inside each command

`validateKnownFields` runs **before** `collectUDAs`. Rationale: an unknown-field error (typo on a reserved name) is a higher-signal failure than a UDA validation error, and the reserved-set check is cheap. Picking one order and applying it consistently in both commands keeps behavior predictable.

### Cobra unknown-flag behavior

Stale invocations (`--uda env=prod`, `-u env=prod`) hit Cobra's standard "unknown flag" / "unknown shorthand flag" errors. No `SuggestFor` wiring or custom unknown-flag hint. The inline form is documented in `--help` output and in the doc sweep, which is enough surface for a pre-release break.

## Helpers and command wiring

### Helper signatures (`internal/tui/uda.go`, rewritten)

```go
// reservedTaskFields is the allowlist for tusk task create / modify.
var reservedTaskFields = map[string]struct{}{
    "title": {}, "description": {}, "project": {},
    "priority": {}, "status": {}, "due": {}, "parent": {},
}

// collectUDAs walks fs.Fields and returns a map of UDA key -> value for
// every field whose key begins with "uda.". Returns nil (not empty map)
// when no uda.* fields are present. Rejects modifier-carrying uda fields
// and keys that fail domain.ValidateUDAKey.
func collectUDAs(fs *filter.FilterSet) (map[string]any, error)

// validateKnownFields returns an error if any field in fs.Fields is not
// in reservedTaskFields and does not have a "uda." prefix. Runs before
// collectUDAs so reserved-set typos surface before UDA-specific errors.
func validateKnownFields(fs *filter.FilterSet) error
```

`collectUDAs` owns prefix check, modifier rejection, `ValidateUDAKey` call, and last-wins merge. `validateKnownFields` owns only the allowlist check and the `did-you-mean uda.X` hint for bare unknowns.

### `runCreate` rewiring

In the existing field-handling block, after the `parent` branch, insert:

```go
if err := validateKnownFields(fs); err != nil {
    return err
}
udaMap, err := collectUDAs(fs)
if err != nil {
    return err
}
if udaMap != nil {
    task.UDA = udaMap
}
```

Remove:

- `createCmd.Flags().StringArrayP("uda", "u", nil, ...)` declaration in the Cobra wiring.
- The `cmd.Flags().Changed("uda")` / `parseUDAFlags` block (currently lines ~240–248 in `internal/tui/commands.go`).

### `runModify` rewiring

Same helper pair, called after the `parent` branch:

```go
if err := validateKnownFields(fs); err != nil {
    return err
}
udaMap, err := collectUDAs(fs)
if err != nil {
    return err
}
if udaMap != nil {
    upd.UDA = &udaMap
}
```

Remove:

- `modifyCmd.Flags().StringArrayP("uda", "u", nil, ...)` declaration.
- The `cmd.Flags().Changed("uda")` / `parseUDAFlags` block (currently lines ~548–556).

### `parseUDAFlags` deletion

Once both commands have moved, `parseUDAFlags` has no in-tree callers. Delete the function and the test cases targeting it in the same commit that rewires the second command. Leaving dead code behind would be caught by the v0.11 cleanup pass, so ship it cleanly in one step.

## MCP parity

MCP tool schemas for `tusk_task_create` and `tusk_task_modify` already accept `uda` as a structured JSON object (`map[string]any`). Agents pass UDAs directly on that field; no dotted-key translation is needed on the MCP surface, and the CLI inline-syntax path is not wired through.

Verification pass — expected to return nothing:

```
grep -rn "parseUDAFlags\|collectUDAs\|validateKnownFields" internal/mcp/
grep -rn "uda\\." internal/mcp/
```

The first grep guards against any accidental import of the new helpers into the MCP layer. The second guards against a handler sneaking dotted-key interpretation into the MCP path while the CLI was being rewired.

## Testing

### Unit tests (`internal/tui/uda_test.go`, rewritten)

`collectUDAs`:

- Empty `FilterSet` → `(nil, nil)`.
- Single `uda.env=prod` → `{"env": "prod"}`.
- Multiple keys `uda.env=prod uda.region=eu` → both entries present.
- Duplicate keys `uda.env=a uda.env=b` → last-wins, `{"env": "b"}`.
- Empty value `uda.env=` → `{"env": ""}`.
- Invalid tail `uda.1env=x` → error routed through `ValidateUDAKey`.
- Empty tail `uda.=x` → error routed through `ValidateUDAKey`.
- Dotted tail `uda.a.b=x` → error routed through `ValidateUDAKey`.
- Modifier on uda field — `+uda.env=prod` and `-uda.env=prod` → error mentioning "modifier".
- Mixed reserved + uda fields → only the uda entries are collected; reserved fields are ignored by this helper.

`validateKnownFields`:

- All-reserved → nil.
- All-uda → nil.
- Mixed reserved + uda → nil.
- Bare unknown `env=prod` → error contains `unknown field` and `did you mean uda.env?`.
- Dotted unknown `foo.bar=1` → error contains `unknown field` and no `did you mean` hint.
- Empty `FilterSet` → nil.

### E2E tests (`tests/e2e/`)

New scenarios, added alongside any existing UDA scenarios. All run through the standard harness (four modes via DB config × output format):

- `task create "title" uda.env=prod uda.region=eu` → `info` shows both.
- `task modify $0.short_id uda.team=backend` → `info` shows the merged set; `env` and `region` preserved.
- `task modify $0.short_id uda.env=` → `info` no longer shows `env`; other keys preserved.
- `task create "title" uda.1env=x` → error, exit non-zero.
- `task create "title" env=prod` → error containing `unknown field` and `did you mean uda.env?`.
- `task create "title" foo.bar=1` → error containing `unknown field`, no hint.
- `task create "title" +uda.env=prod` → error containing "modifier".
- `task create "title" --uda env=prod` → Cobra "unknown flag" error (locks in flag removal).
- `task create "title" -u env=prod` → Cobra "unknown shorthand flag" error.
- `task create "title" uda.env=a uda.env=b` → `info` shows `env=b` (last-wins).

### MCP grep verification

Run the grep commands listed above and confirm they return nothing. No Go test — this is a human-eye gate on the breaking-change PR.

## Documentation changes

- `PRODUCT.md` — the CLI examples section already shows `uda.env=prod` in filter syntax. Add a matching example on the `tusk task create` / `tusk task modify` lines, and add a one-sentence note to the "Inline Syntax" principle paragraph: "`uda.key=value` on create and modify sets a user-defined attribute; an empty value on modify deletes the key." Remove any lingering `--uda` references if the sweep finds them.
- `ROADMAP.md` — tick the three stories under the "UDA Flag Elimination" initiative.
- `docs/configuration.md` — grep for `--uda`; rewrite any hit to the inline form. Expected: zero hits.
- `docs/releases/v0.11.md`, `docs/status/v0.11-status.md` — **not touched**. Status and release docs land at milestone completion per the status-files-milestone-only convention.

## Risks and open questions

- **Broader unknown-field rejection is a behavior change.** Fields currently silently ignored on create/modify (e.g. `waiting=true`, `tree=...`) will error after this initiative lands. Accepted cost — the roadmap text is explicit, the inline-field principle already says there's one way to set a field on a task, and this is a pre-release milestone so no back-compat aliases are owed.
- **Pre-release scripts using `--uda`.** Users see Cobra's standard "unknown flag" error. No custom suggestion shim. The inline form is documented in help text and the doc sweep; that is enough surface for a pre-release.
- **UDA delete-on-create not reachable.** `uda.env=` on create stores an empty string; on modify it deletes. This pre-existing asymmetry is preserved — the fix would require either a new CLI gesture for create or a service-layer behavior change, both of which are out of scope.
- **Dotted UDA keys.** `uda.a.b=x` is rejected by `ValidateUDAKey`. If future needs want nested UDA keys, that is a domain-layer change, not a CLI one.
- **Ordering of `validateKnownFields` vs `collectUDAs`.** The design runs `validateKnownFields` first so reserved-set typos surface before UDA-specific errors. The opposite order would be defensible (surface bad UDA keys first), but consistent ordering matters more than which order is picked — the phase plan must apply it to both commands identically.

## Implementation sequence

Rough order for the phase plan (drafted separately):

1. Add `collectUDAs` and `validateKnownFields` helpers in `internal/tui/uda.go` with full unit coverage. Old `parseUDAFlags` still in place, unused from the new helpers.
2. Rewire `runCreate` to call the new helpers; delete the `--uda` flag declaration and the `parseUDAFlags` call from `runCreate`.
3. Rewire `runModify` the same way. Delete `parseUDAFlags` itself and its test cases — no callers remain.
4. Add the E2E scenarios.
5. MCP grep verification pass.
6. Doc sweep (`PRODUCT.md`, `ROADMAP.md`, `docs/configuration.md` if needed).

Each step is independently reviewable. Step 2 is the breaking-change gate for `create`; step 3 is the gate for `modify` and the `parseUDAFlags` deletion.
