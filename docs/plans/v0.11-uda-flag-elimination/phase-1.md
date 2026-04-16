# Phase 1 — UDA helpers and unit coverage

**Milestone:** v0.11
**Initiative:** UDA Flag Elimination
**Phase:** 1 of 3
**Prerequisites:** None beyond current `main`.
**Parallelizable:** No — Phase 2 depends on the helpers landed here.

## Intent

Introduce the two consumer-layer helpers that the rewire phase will use: `collectUDAs` and `validateKnownFields`, plus the `reservedTaskFields` allowlist they share. Full unit coverage lands in this phase so the behavioral contract is locked in before any command is rewired to call them. The existing `parseUDAFlags` function and its tests remain in place and continue to back the `--uda` / `-u` flags — this phase does **not** change any user-visible CLI behavior.

At the end of Phase 1 the helpers are package-level symbols in `internal/tui/uda.go`, the unit test suite covers every branch of the design, and `go build ./...` plus `go test ./internal/tui` are green. No command in the CLI calls the new helpers yet.

## Tasks

### 1. Add `reservedTaskFields` and `collectUDAs` to `internal/tui/uda.go`

Open `internal/tui/uda.go`. Append the following package-level symbols **below** the existing `parseUDAFlags` function (do not delete or modify `parseUDAFlags`):

```go
// reservedTaskFields is the allowlist of top-level field keys that
// tusk task create and tusk task modify accept in their inline syntax.
// Any other bare key is rejected as unknown by validateKnownFields;
// uda.* fields are handled separately by collectUDAs.
var reservedTaskFields = map[string]struct{}{
    "title":       {},
    "description": {},
    "project":     {},
    "priority":    {},
    "status":      {},
    "due":         {},
    "parent":      {},
}

// collectUDAs walks fs.Fields and returns a map of UDA key -> value
// for every field whose key begins with "uda.". The returned map is
// nil (not an empty map) when no uda.* field is present, so callers
// can distinguish "caller touched UDAs" from "caller did not".
//
// Duplicates resolve last-wins, matching the StringArray semantics
// of the old --uda flag.
//
// Errors:
//   - a uda.* field carrying a non-zero Modifier prefix is rejected
//     ("modifier %q not supported on uda fields", string(prefix))
//   - the tail after "uda." is validated via domain.ValidateUDAKey,
//     which rejects empty, digit-led, and dot-containing tails
func collectUDAs(fs *filter.FilterSet) (map[string]any, error) {
    var out map[string]any
    for _, f := range fs.Fields {
        key, ok := strings.CutPrefix(f.Key, "uda.")
        if !ok {
            continue
        }
        if f.Modifier != 0 {
            return nil, fmt.Errorf("modifier %q not supported on uda fields", string(f.Modifier))
        }
        if err := domain.ValidateUDAKey(key); err != nil {
            return nil, err
        }
        if out == nil {
            out = make(map[string]any)
        }
        out[key] = f.Value
    }
    return out, nil
}
```

The existing imports in `uda.go` already cover `fmt`, `strings`, and `github.com/germanamz/tusk/domain`. Add an import for `github.com/germanamz/tusk/filter` — the helper takes `*filter.FilterSet`. If the exact filter package path differs from what is shown here, grep `internal/tui/commands.go` for the `filter.Parse(` call site and use the same import path.

Do not touch `parseUDAFlags`. It still has callers in `commands.go` and must keep working.

### 2. Add `validateKnownFields` to `internal/tui/uda.go`

Append **immediately below** `collectUDAs`:

```go
// validateKnownFields returns an error if any field in fs.Fields is
// not in reservedTaskFields and does not have a "uda." prefix.
//
// Bare unknown keys (no dot) return a "did you mean uda.X?" hint
// to catch the common typo where a user forgets the uda prefix.
// Dotted unknown keys return a plain "unknown field" error — a dot
// in the key signals intent, so a did-you-mean hint would be noise.
func validateKnownFields(fs *filter.FilterSet) error {
    for _, f := range fs.Fields {
        if _, ok := reservedTaskFields[f.Key]; ok {
            continue
        }
        if strings.HasPrefix(f.Key, "uda.") {
            continue
        }
        if strings.Contains(f.Key, ".") {
            return fmt.Errorf("unknown field %q", f.Key)
        }
        return fmt.Errorf("unknown field %q; did you mean uda.%s?", f.Key, f.Key)
    }
    return nil
}
```

### 3. Rewrite `internal/tui/uda_test.go` around the new helpers

Open `internal/tui/uda_test.go`. Keep any pre-existing `parseUDAFlags` tests **intact** — the function is still live and must still be exercised. Add new test functions alongside them:

`TestCollectUDAs`:

- Empty `FilterSet` → `(nil, nil)`.
- Single field `uda.env=prod` → `map[string]any{"env": "prod"}`, no error.
- Two fields `uda.env=prod`, `uda.region=eu` → both entries, no error.
- Duplicate `uda.env=a`, `uda.env=b` → `{"env": "b"}` (last-wins).
- Empty value `uda.env=` → `{"env": ""}` (no special-casing).
- Invalid tail `uda.1env=x` → error is non-nil (domain regex rejection). No need to match the exact message — use `err == nil` / `err != nil` plus a substring check for `"UDA"` or `"uda"` so the test is resilient to upstream message tweaks.
- Empty tail `uda.=x` → error is non-nil.
- Dotted tail `uda.a.b=x` → error is non-nil.
- Modifier `+` on `uda.env=prod` → error contains substring `"modifier"`.
- Modifier `-` on `uda.env=prod` → error contains substring `"modifier"`.
- Mixed reserved + uda fields (e.g. `title=t`, `uda.env=prod`, `priority=3`) → map contains only `env`, no error.

`TestValidateKnownFields`:

- Empty `FilterSet` → nil error.
- All-reserved (`title=t`, `project=p`, `priority=3`) → nil error.
- All-uda (`uda.env=prod`, `uda.region=eu`) → nil error.
- Mixed reserved + uda → nil error.
- Bare unknown `env=prod` → error contains `"unknown field"` **and** `"did you mean uda.env?"`.
- Bare unknown `waiting=true` → error contains the did-you-mean hint for `uda.waiting`.
- Dotted unknown `foo.bar=1` → error contains `"unknown field"` and does **not** contain `"did you mean"`.
- `tree=abc` → error (tree is filter-only, not in the reserved set) with did-you-mean hint `uda.tree`.

Construct `filter.FilterSet` literals directly for the tests — do not route through `filter.Parse`. The helpers operate on the parsed structure, and using literals keeps the tests focused on helper behavior rather than the lexer. Example pattern used elsewhere in `filter/resolve_uda_test.go`:

```go
fs := filter.FilterSet{Fields: []filter.FieldFilter{
    {Key: "uda.env", Value: "prod"},
    {Key: "uda.region", Value: "eu"},
}}
got, err := collectUDAs(&fs)
```

For modifier cases, set `Modifier: '+'` on the literal.

### 4. Verify build, vet, and tests

Run from the repository root:

```
go build ./...
go vet ./...
go test ./internal/tui ./domain
```

All three must succeed. The expected assertion is that `parseUDAFlags` is still callable and its pre-existing tests still pass — do not delete them in this phase. If vet flags `collectUDAs` or `validateKnownFields` as unused, that means the test file is importing them under a different name or missing a reference; fix the test file rather than suppressing the warning.

If the build fails because the `filter` package is not yet imported in `internal/tui/uda.go`, add the import. If it fails because `domain` is not yet imported (it already is for `parseUDAFlags`), reconfirm the import list after the edits.

## Acceptance criteria

User-visible behavior that must still work after Phase 1:

- `tusk task create --uda env=prod "..."` still creates a task with `env=prod` attached. Flag and helper unchanged.
- `tusk task modify <id> --uda env=` still deletes the `env` UDA via the existing service merge semantics.
- `tusk task create foo=bar "title"` still silently ignores `foo=bar` (this is the pre-existing behavior — Phase 2 changes it, not Phase 1).
- `tusk task create uda.env=prod "title"` still silently ignores `uda.env=prod` — the new helper is defined but not wired into `runCreate` yet.
- `go build ./... && go test ./...` both green.

If any of these are no longer true at the end of Phase 1, the phase is incorrectly scoped — stop and flag it.

## Changes Introduced

**New package-level symbols (`internal/tui/uda.go`):**
- `reservedTaskFields` — `map[string]struct{}` of allowlisted top-level field keys.
- `collectUDAs(fs *filter.FilterSet) (map[string]any, error)`.
- `validateKnownFields(fs *filter.FilterSet) error`.

**New imports (`internal/tui/uda.go`):**
- `github.com/germanamz/tusk/filter` (if not already imported).

**Modified files:**
- `internal/tui/uda.go` — additive edits below the existing `parseUDAFlags` function.
- `internal/tui/uda_test.go` — additive test functions (`TestCollectUDAs`, `TestValidateKnownFields`). Pre-existing `parseUDAFlags` tests left untouched.

**Bridge code:** None. The new helpers are live symbols with unit coverage but no CLI caller until Phase 2. This is intentional unused code for exactly one phase; Go does not fail on unused package-level functions, so no compilation workaround is needed.

**Not changed in this phase:**
- `parseUDAFlags` and its existing callers in `runCreate` / `runModify`.
- Cobra `--uda` / `-u` flag declarations on `createCmd` and `modifyCmd`.
- Any file under `internal/mcp/`.
- Any documentation.
