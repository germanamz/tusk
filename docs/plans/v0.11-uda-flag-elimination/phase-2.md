# Phase 2 — Command rewire, flag removal, E2E coverage

**Milestone:** v0.11
**Initiative:** UDA Flag Elimination
**Phase:** 2 of 3
**Prerequisites:** Phase 1 must be complete.
**Parallelizable:** No — Phase 3 (doc sweep) depends on the behavior changes in this phase.

## Inherits From

Phase 1 added three symbols to `internal/tui/uda.go`:

- `reservedTaskFields` — `map[string]struct{}` of allowlisted keys.
- `collectUDAs(fs *filter.FilterSet) (map[string]any, error)` — extracts `uda.*` entries from a `FilterSet`.
- `validateKnownFields(fs *filter.FilterSet) error` — rejects unknown top-level fields.

Both helpers have full unit coverage. `parseUDAFlags` and the `--uda` / `-u` flag declarations on `createCmd` / `modifyCmd` are still present and still back the current behavior. This phase replaces them.

## Intent

Wire the Phase-1 helpers into `runCreate` and `runModify`, delete `--uda` / `-u` from both commands, delete `parseUDAFlags` and its test cases, then lock in the new behavior with E2E scenarios and an MCP verification grep. This is the breaking-change phase — at the end of it, `uda.key=value` inline syntax is the only way to set UDAs from the CLI, unknown top-level fields are rejected loudly, and stale `--uda` invocations hit Cobra's standard "unknown flag" error.

## Tasks

### 1. Rewire `runCreate` in `internal/tui/commands.go`

Open `internal/tui/commands.go` and locate `runCreate`.

**Find the `--uda` block** (currently around lines 240–248):

```go
// UDA
if cmd.Flags().Changed("uda") {
    udaVals, _ := cmd.Flags().GetStringArray("uda")
    udaMap, err := parseUDAFlags(udaVals)
    if err != nil {
        return err
    }
    task.UDA = udaMap
}
```

**Replace it with** the following, in the same location (after the `parent` field-handling branch, before the `a.taskSvc.Create` call):

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

**Remove the `--uda` flag declaration.** Search `commands.go` (or `app.go`, depending on where Cobra wiring lives) for the Cobra flag registration on the create command. It will look like one of:

```go
createCmd.Flags().StringArrayP("uda", "u", nil, "...")
```

Delete that line. If the flag setup is in a different file, search with `grep -rn '"uda"' internal/tui/` to locate it.

### 2. Rewire `runModify` in `internal/tui/commands.go`

Same pattern. Locate `runModify`.

**Find the `--uda` block** (currently around lines 548–556):

```go
// UDA
if cmd.Flags().Changed("uda") {
    udaVals, _ := cmd.Flags().GetStringArray("uda")
    udaMap, err := parseUDAFlags(udaVals)
    if err != nil {
        return err
    }
    upd.UDA = &udaMap
}
```

**Replace it with:**

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

**Remove the `--uda` flag declaration** from the modify command's Cobra wiring. Same search approach as Task 1 — likely a one-liner `modifyCmd.Flags().StringArrayP("uda", "u", nil, "...")`.

### 3. Delete `parseUDAFlags` and its test cases

After Tasks 1 and 2, `parseUDAFlags` has no callers in the codebase. Delete it cleanly.

**In `internal/tui/uda.go`:**
- Delete the `parseUDAFlags` function entirely (currently lines ~10–33).
- If the deletion leaves any imports unused (e.g. `strings` was only used by `parseUDAFlags`), remove those imports. Check: `collectUDAs` uses `strings.CutPrefix` and `validateKnownFields` uses `strings.HasPrefix` and `strings.Contains`, so `strings` stays.

**In `internal/tui/uda_test.go`:**
- Delete every test function that tests `parseUDAFlags` (look for test names containing `ParseUDA` or `parseUDA`).
- Leave the `TestCollectUDAs` and `TestValidateKnownFields` functions added in Phase 1.
- After deletion, verify the test file still compiles: `go test -run=XXX_NOMATCH ./internal/tui` (runs zero tests but checks compilation).

**Verification:** `grep -rn "parseUDAFlags" .` should return zero hits.

### 4. Add E2E test scenarios

Add new E2E scenarios in `tests/e2e/`. Place them in a file that covers UDA behavior — look at the existing test files for the naming convention (likely a task CRUD or modify scenario file). Use the existing harness conventions: `Scenario` structs, `Step` arrays, `$0.short_id` reference syntax, `ExpectError: true` for error cases.

Required scenarios:

**Happy path — create with multiple UDAs:**
```
Step 0: task create "UDA inline test" uda.env=prod uda.region=eu
Step 1: task get $0.short_id
  → assert output contains "env" and "prod"
  → assert output contains "region" and "eu"
```

**Modify — add a UDA to existing task:**
```
Step 0: task create "UDA modify base" uda.env=prod
Step 1: task modify $0.short_id uda.team=backend
Step 2: task get $0.short_id
  → assert output contains "env" and "prod" (preserved)
  → assert output contains "team" and "backend" (added)
```

**Modify — delete a UDA via empty value:**
```
Step 0: task create "UDA delete test" uda.env=prod uda.region=eu
Step 1: task modify $0.short_id uda.env=
Step 2: task get $0.short_id
  → assert output does NOT contain "env"
  → assert output contains "region" and "eu" (preserved)
```

**Duplicate key last-wins:**
```
Step 0: task create "UDA dup test" uda.env=a uda.env=b
Step 1: task get $0.short_id
  → assert output contains "env" and "b" (last-wins)
```

**Error — invalid UDA key:**
```
Step 0: task create "bad key" uda.1env=x
  → ExpectError: true, assert stderr contains "UDA" or "uda"
```

**Error — unknown top-level field with hint:**
```
Step 0: task create "unknown field" env=prod
  → ExpectError: true, assert stderr contains "unknown field" AND "did you mean uda.env?"
```

**Error — unknown dotted field without hint:**
```
Step 0: task create "dotted unknown" foo.bar=1
  → ExpectError: true, assert stderr contains "unknown field"
  → assert stderr does NOT contain "did you mean"
```

**Error — modifier on uda field:**
```
Step 0: task create "modifier uda" +uda.env=prod
  → ExpectError: true, assert stderr contains "modifier"
```

**Error — stale `--uda` flag:**
```
Step 0: task create --uda env=prod "stale flag"
  → ExpectError: true, assert stderr contains "unknown flag"
```

**Error — stale `-u` shorthand:**
```
Step 0: task create -u env=prod "stale shorthand"
  → ExpectError: true, assert stderr contains "unknown"
```

Match the exact assertion patterns used by other E2E error tests in the repo. If the harness uses `ContainsStr` or a similar helper, use the same call shape. If assertions are on `Step.ExpectOutput` or `Step.ExpectErr`, follow that convention.

### 5. MCP verification grep and final check

Run the following from the repository root and confirm each returns zero hits:

```bash
grep -rn "parseUDAFlags\|collectUDAs\|validateKnownFields" internal/mcp/
grep -rn "uda\\\." internal/mcp/
```

The first confirms no CLI helper leaked into MCP. The second confirms no MCP handler grew dotted-key interpretation during the rewire.

Then run the full test suite:

```bash
go build ./...
go vet ./...
go test ./...
```

All three must pass. If any E2E scenario fails, fix it before closing the phase.

## Acceptance criteria

User-visible behaviors that must work after Phase 2:

- `tusk task create "title" uda.env=prod uda.region=eu` → task created, `tusk task get` shows both UDAs.
- `tusk task modify <id> uda.team=backend` → task updated, existing UDAs preserved, new one added.
- `tusk task modify <id> uda.env=` → `env` key deleted, other UDAs preserved.
- `tusk task create "title" env=prod` → error with "unknown field" and "did you mean uda.env?".
- `tusk task create "title" foo.bar=1` → error with "unknown field", no hint.
- `tusk task create "title" +uda.env=prod` → error mentioning "modifier".
- `tusk task create "title" --uda env=prod` → Cobra "unknown flag" error.
- `tusk task create "title" -u env=prod` → Cobra "unknown shorthand flag" error.
- All existing E2E tests pass — the unknown-field rejection should not break any scenario that was previously green. If a scenario was passing by accident with an unknown field, the implementer should fix the scenario.
- All MCP tools for task create/modify continue to work unchanged — uda object field in JSON, no dotted-key handling.

## Changes Introduced

**Modified files:**
- `internal/tui/commands.go` — `runCreate` and `runModify` rewired to call `validateKnownFields` and `collectUDAs`. `--uda` / `-u` flag declarations removed from both commands.
- `internal/tui/uda.go` — `parseUDAFlags` function deleted.
- `internal/tui/uda_test.go` — `parseUDAFlags` test cases deleted. Phase-1 tests for `collectUDAs` and `validateKnownFields` remain.
- `tests/e2e/` — new scenario file or additions to existing file covering all paths listed above.

**Removed symbols:**
- `parseUDAFlags` (was `internal/tui/uda.go`).

**Bridge code:** None introduced. No bridge code from Phase 1 to remove — Phase 1's helpers were not bridge code, they were production code waiting for wiring.

**Not changed in this phase:**
- Any file under `internal/mcp/`.
- `PRODUCT.md`, `ROADMAP.md`, `docs/configuration.md` — documentation sweep is Phase 3.
