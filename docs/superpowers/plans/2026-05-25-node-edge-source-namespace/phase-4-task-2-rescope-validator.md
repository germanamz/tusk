# Phase 4 — Task 2: Rescope `SubUnitConflict` validator

**Phase:** 4 (Reservation rescoping)
**Spec:** § *Type-pack reservation model*

**Goal:** Make the manifest validator fire `SubUnitConflict` only when a declaration collides with the pack's reservations *within the same source*. User-namespace (`source=NULL`) declarations of `section`, `paragraph`, `contains`, etc., are allowed.

## Inherits From

After Task 4.1:
- `subdocument.Source()` returns `"markdown"`.
- `ReservedNodeTypes` and `ReservedEdgeTypes` documented as source-scoped.
- The validator (in `internal/manifest/subunits.go` or `internal/manifest/loader.go`) currently raises `SubUnitConflict` for any user declaration matching a reserved name.

## Files

- **Modify:** `internal/manifest/subunits.go` (or wherever `SubUnitConflict` is constructed)
- **Modify/add:** `internal/manifest/subunits_test.go`

## Steps

- [ ] **Step 1: Locate the validator**

Run:
```
grep -rn 'SubUnitConflict\|reserved.*conflict' internal/manifest
```

Identify the function that constructs `SubUnitConflict`. Typically it walks `loaded.NodeTypes` and `loaded.EdgeTypes`, comparing each user-declared name against the reserved lists.

- [ ] **Step 2: Write the failing test**

In `internal/manifest/subunits_test.go`:

```go
func TestUserNamespaceSectionDeclarationLoadsClean(test *testing.T) {
	test.Parallel()

	const manifestText = `[workspace]
name = "test"
sub-units = true

[node-types.section]

[node-types.section.properties.summary]
type = "string"
`

	tempFile := filepath.Join(test.TempDir(), "tusk.toml")
	if writeErr := os.WriteFile(tempFile, []byte(manifestText), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(test.TempDir())
	if loadErr != nil {
		test.Fatalf("manifest.Load: %v — should accept user-namespace section node-type", loadErr)
	}

	if _, exists := loaded.NodeTypes["section"]; !exists {
		test.Error("loaded.NodeTypes is missing 'section'")
	}
}

func TestUserNamespaceContainsEdgeDeclarationLoadsClean(test *testing.T) {
	test.Parallel()

	const manifestText = `[workspace]
name = "test"
sub-units = true

[edge-types.contains]
from = ["project"]
to   = ["task"]
`

	tempFile := filepath.Join(test.TempDir(), "tusk.toml")
	if writeErr := os.WriteFile(tempFile, []byte(manifestText), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(test.TempDir())
	if loadErr != nil {
		test.Fatalf("manifest.Load: %v — should accept user-namespace contains edge-type", loadErr)
	}

	if _, exists := loaded.EdgeTypes["contains"]; !exists {
		test.Error("loaded.EdgeTypes is missing 'contains'")
	}
}
```

(Use the existing test file's manifest fixture helper if one exists; the literal TOML above is a fallback.)

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/manifest/... -run 'TestUserNamespace' -v`

Expected: FAIL — current validator raises `SubUnitConflict`.

- [ ] **Step 4: Rescope the validator**

Find the validator code. The current logic looks roughly like:

```go
for name := range loaded.NodeTypes {
    if reservedSet[name] {
        return &SubUnitConflict{Type: "node", Name: name}
    }
}
```

User declarations live in the user namespace (`source = NULL`). Pack reservations live in `source = subdocument.Source()`. They are in different namespaces, so the comparison should never fire across them.

Rewrite:

```go
// Pack reservations are scoped to subdocument.Source(); user
// declarations live at source=NULL. The two namespaces are
// independent, so a user-declared name that matches a pack
// reservation does NOT conflict.
//
// SubUnitConflict still fires for true within-source collisions:
// the future user-configurable-sources extension may introduce
// declarations at a specific source matching a pack reservation in
// the same source — that is what this validator now guards.

// Synthesized validation: walk only declarations whose source matches
// the pack's. Today no manifest grammar produces such declarations,
// so this loop is a no-op until that extension lands. We keep the
// validator structure in place so the wiring is ready.
for _, decl := range loaded.NodeTypes {
    if decl.Source != subdocument.Source() {
        continue
    }
    if reservedNodeSet[decl.Name] {
        return &SubUnitConflict{Type: "node", Name: decl.Name, Source: decl.Source}
    }
}
```

The `decl.Source` field does not exist on the current `NodeType` struct; today every user declaration is in the user namespace. The simplest correct change is to remove the user-vs-pack conflict check entirely and add a comment:

```go
// SubUnitConflict no longer fires for user-vs-pack collisions —
// user declarations live at source=NULL and pack declarations live
// at source=subdocument.Source(); they cannot collide.
//
// The validator structure stays in place to guard within-source
// collisions if the user-configurable-sources extension lands. As
// of today no manifest grammar can produce a within-source user
// declaration, so this is effectively a no-op.
```

Then drop the conflict construction in the user-namespace path. Apply the same change to the edge-type path.

- [ ] **Step 5: Add a synthetic within-source test**

To prove the validator still works for within-source collisions when future grammar enables it, write a unit test against a hand-constructed `Manifest` struct that simulates a within-source declaration. This may require a test-only helper or unexported `setSource` method on `NodeType`. If neither exists and adding one is out of scope, leave a regression test for the user-vs-pack rescue case only and add a TODO referencing the user-configurable-sources future-extension spec.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/manifest/... -run 'TestUserNamespace' -v`

Expected: PASS.

- [ ] **Step 7: Run the workspace suite**

Run: `go test ./...`

Expected: clean. Some pre-existing tests may have asserted that user-namespace `section` raises `SubUnitConflict` — those tests need to be updated to assert the new behavior (no conflict). The implementer must identify and update each.

- [ ] **Step 8: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 9: Commit**

```
git add internal/manifest
git commit -m "feat(manifest): SubUnitConflict no longer fires for user-vs-pack collisions"
```

- [ ] **Step 10: Open the PR**

```
gh pr create --title "feat(manifest): SubUnitConflict no longer fires for user-vs-pack collisions" --body "$(cat <<'EOF'
## Summary
- `SubUnitConflict` validator rescoped: user-namespace declarations of names that the subdocument pack reserves are now accepted
- Pre-existing tests asserting the old behavior updated
- Validator structure preserved for future user-configurable-sources extensions
- Phase 4, Task 2 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/manifest/... -v` passes (incl. new user-namespace tests)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- User-namespace tests pass.
- Workspace suite green (with any necessary pre-existing test updates).
- PR open.
