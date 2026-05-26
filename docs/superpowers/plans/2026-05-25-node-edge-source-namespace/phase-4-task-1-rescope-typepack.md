# Phase 4 — Task 1: Rescope `subdocument` typepack reservations

**Phase:** 4 (Reservation rescoping)
**Spec:** § *Type-pack reservation model*

**Goal:** Update the documentation and any structural representation of `ReservedNodeTypes`/`ReservedEdgeTypes` in `internal/typepacks/subdocument/pack.go` so the lists describe names owned within `source='markdown'`, not globally. The slice values themselves stay the same — what changes is how callers interpret them.

If the typepack exposes a `ScopedReservation` struct or similar (it does not today), introduce it now so callers in Task 4.2 have a clean API.

## Inherits From

- Phase 3 complete; index has `(kind, source)` on both tables.
- `subdocument.ReservedNodeTypes` is a `[]string` of six names (`section`, `paragraph`, …).
- `subdocument.ReservedEdgeTypes` is a `[]string` of two names (`contains`, `contained-by`).
- The validator (Task 4.2 target) currently consumes these slices as a global blocklist.

## Files

- **Modify:** `internal/typepacks/subdocument/pack.go`
- **Modify/add:** `internal/typepacks/subdocument/pack_test.go`

## Steps

- [ ] **Step 1: Decide on the API**

Two reasonable shapes:

**(a) Keep current `[]string` values; add a `Source()` accessor returning `"markdown"`.** Simplest. The validator (Task 4.2) reads both `ReservedNodeTypes` and `Source()` to scope the check.

**(b) Introduce `ScopedReservation` struct.** Forward-looking — future packs could implement the same interface, and the validator would loop over packs.

Pick **(a)** for this spec — it is the minimum change to enable source-scoped validation. Option (b) is a structural refactor that belongs in the follow-up source-package consolidation spec.

- [ ] **Step 2: Write the failing test**

In `internal/typepacks/subdocument/pack_test.go`:

```go
func TestSubdocumentSourceIsMarkdown(test *testing.T) {
	test.Parallel()

	if subdocument.Source() != "markdown" {
		test.Errorf("Source() = %q, want %q", subdocument.Source(), "markdown")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/typepacks/subdocument/... -v`

Expected: FAIL — `Source()` undefined.

- [ ] **Step 4: Add `Source()` and update doc comments**

In `internal/typepacks/subdocument/pack.go`:

```go
// Source returns the source-namespace identifier this typepack
// owns: `"markdown"`. Every node-type in ReservedNodeTypes and every
// edge-type in ReservedEdgeTypes is reserved only within rows whose
// `source` column matches this value; the user namespace
// (`source = NULL`) is unaffected.
func Source() string {
	return "markdown"
}
```

Update the doc comment above `ReservedNodeTypes`:

```go
// ReservedNodeTypes are the node-type names owned by the sub-document
// pack within source = Source() (i.e., source='markdown'). A user
// manifest that declares any of these under [node-types.<name>] in
// the user namespace (source = NULL) is allowed; only declarations
// targeting the same source raise SubUnitConflict (rescoped in
// Phase 4, Task 2).
var ReservedNodeTypes = []string{...} // unchanged values
```

And above `ReservedEdgeTypes`:

```go
// ReservedEdgeTypes are the edge-type names owned by this pack
// within source = Source(). User-namespace declarations
// (source = NULL) of the same names are allowed; only within-source
// duplicates raise a conflict.
var ReservedEdgeTypes = []string{...} // unchanged values
```

- [ ] **Step 5: Run the test**

Run: `go test ./internal/typepacks/subdocument/... -v`

Expected: PASS.

- [ ] **Step 6: Run the workspace suite**

Run: `go test ./...`

Expected: clean — the validator (Task 4.2) still uses the global semantics, so existing tests pass. This task changes documentation and API surface only; the validator change comes next.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/typepacks/subdocument
git commit -m "feat(typepacks): subdocument exposes Source() and source-scoped reservation semantics"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(typepacks): subdocument exposes Source() and source-scoped reservation semantics" --body "$(cat <<'EOF'
## Summary
- Adds `subdocument.Source()` returning `"markdown"`
- Reframes `ReservedNodeTypes` and `ReservedEdgeTypes` doc comments to describe source-scoped semantics
- No behavioral change yet — the validator (Task 4.2) still uses global semantics
- Phase 4, Task 1 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/typepacks/subdocument/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- `Source()` returns `"markdown"`.
- Doc comments reflect source-scoped semantics.
- PR open.
