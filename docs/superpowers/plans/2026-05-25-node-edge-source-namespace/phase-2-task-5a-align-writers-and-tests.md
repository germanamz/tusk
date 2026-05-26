# Phase 2 — Task 5a: Align writers in tests, drop now-impossible coverage

**Phase:** 2 (Nodes table reshape)
**Spec:** § *Nodes table*, § *Index schema bump & rebuild*

**Goal:** Prepare the workspace for the CHECK constraint added in Task 2.5b. Today many tests reach the `nodes` table via the wrong writer (`Upsert` for sub-unit-shaped rows, `BulkUpsert` for file-shaped rows) or via direct DDL without the new `kind`/`source` columns. Under the loose Phase 2.1–2.4 DDL this works; under the tightened DDL it will not. Fix every test to call the correct writer for the row shape it constructs, fix the two direct-DDL inserts, and delete the two tests whose constructed states become structurally impossible under the CHECK. After this PR every test still passes under today's loose DDL; Task 2.5b can then promote the schema without further test churn.

## Inherits From

After Task 2.4:
- File-row writer (`NodeRepo.Upsert`) writes `kind='file', source=NULL`.
- Sub-unit writer (`NodeRepo.BulkUpsert`) writes `kind='subunit', source='markdown'`.
- Every read uses `kind` as the row-class discriminator (no `parent_id IS NOT NULL`).
- Schema constants still leave `kind`/`source` nullable; no CHECK constraint yet.

## Why a separate PR

Trial-applying the Task 2.5 DDL surfaces 15 test failures across four packages plus a structural problem in the schema-constant bootstrap. Folding all of those test fixes into the DDL-tightening PR would (1) make a single huge PR that mixes schema work and test refactoring and (2) violate the lefthook pre-commit invariant (`make test`) at every intermediate commit. Landing the test corrections first under the still-loose DDL keeps each commit green and isolates the actual schema change to a small PR.

## Files

- **Modify:** `internal/index/node_repo_test.go`
- **Modify:** `internal/index/index_test.go`
- **Modify:** `internal/embed/drain_test.go`
- **Modify:** `internal/mcp/tools_test.go`
- **Modify:** `internal/query/query_test.go` (and any sibling files in `internal/query/` that touch the same patterns — verify with grep before editing)

No production code changes.

## The rule

The CHECK constraint that lands in Task 2.5b is:

```
CHECK (
    (kind = 'file'    AND source IS NULL     AND parent_id IS NULL) OR
    (kind = 'subunit' AND source IS NOT NULL AND parent_id IS NOT NULL)
)
```

Two writers each pin `kind`/`source` to one branch of the OR:

| Writer | Sets | Caller must supply |
|---|---|---|
| `NodeRepo.Upsert(row)` | `kind='file'`, `source=NULL` | `row.ParentID` MUST be `sql.NullString{Valid:false}` (NULL) |
| `NodeRepo.BulkUpsert(rows)` | `kind='subunit'`, `source='markdown'` | every `row.ParentID` MUST be `sql.NullString{Valid:true,String:<parent id>}` |

Any test that violates this pairing currently happens to work (the schema is lax) but will be rejected by the CHECK in Task 2.5b. **The fix is mechanical, not semantic** — the tests already construct rows that the production codepaths construct via the correct writer; they just call the wrong helper.

## Steps

- [ ] **Step 1: Snapshot the failure set with the future DDL**

To confirm we converge on the full list, temporarily apply Task 2.5b's DDL change locally (without committing) and capture the failing tests. Use a scratch `git stash` so the working tree stays clean.

```bash
# Apply the future DDL just to read the failure set; don't keep this.
git stash push -m "probe" -- internal/index/index.go internal/index/schema_version.go

# In a separate terminal, hand-edit internal/index/index.go to add the
# CHECK + NOT NULL kind + nodes_kind_type_idx (see Task 2.5b for the
# exact diff). Then:
go test ./... 2>&1 | grep -E '^--- FAIL|^FAIL\s' | tee /tmp/check-failures.txt

# Restore:
git checkout internal/index/index.go internal/index/schema_version.go
```

Expected `/tmp/check-failures.txt` (at the time this plan was written; verify on your machine):

```
--- FAIL: TestDrainQueue_SubUnitEmbedsEmbedPayloadDirectly
--- FAIL: TestDrainQueue_SubUnitWithEmptyEmbedPayloadIsSkipped
--- FAIL: TestDrainQueue_MixedFileAndSubUnitBatch
--- FAIL: TestOpen_P2MigrationFromLegacyDB
--- FAIL: TestOpen_CascadeDeleteEdgesAndEmbeddings
--- FAIL: TestNodeRepo_ListByParent
--- FAIL: TestNodeRepo_BulkUpsert
--- FAIL: TestNodeRepo_BulkDelete
--- FAIL: TestNodeRepo_BulkDeleteCascadesEdgesAndEmbeddings
--- FAIL: TestNodeRepo_ListSubUnitsForFile_UnderscoreNoLeakage
--- FAIL: TestNodeRepo_ListSubUnitsForFiles
--- FAIL: TestCountSubUnitsUsesKindNotParentID
--- FAIL: TestTool_Query_SubUnitsIncludeUnitsAttachesMatchedUnits
--- FAIL: TestTool_Query_DirectSubUnitFilterReturnsRowsWithParentID
--- FAIL: TestQueryRun_StructuralIncludeUnitsAttachesSubUnits
--- FAIL: TestQueryRun_DirectSubUnitQueryReturnsRowsWithParentID
--- FAIL: TestQueryRun_SemanticSubUnitsGroupsByParent
```

If your list differs, treat each new failure with the same rule (Step 2) — but verify nothing in the production code regressed in the meantime.

- [ ] **Step 2: Delete the two tests that become structurally impossible**

`TestCountSubUnitsUsesKindNotParentID` (in `internal/index/node_repo_test.go`) was added in Task 2.4 specifically to prove the read uses `kind`, not `parent_id IS NOT NULL`. It seeds a "weird-row" with `parent_id` set but `kind='file'` — exactly the shape the new CHECK forbids. Once the CHECK exists the divergence is impossible at the DDL layer (a strictly stronger guarantee than the unit test), so the test is redundant. Delete the whole function (≈27 lines).

`TestOpen_P2MigrationFromLegacyDB` (in `internal/index/index_test.go`) seeds a pre-P2 schema with no `kind`/`source` columns, then exercises in-place migration through `index.Open`. Per spec § *What the schema bump removes*, the new rebuild model retires this path: any DB lacking the `schema_version` key trips `ErrSchemaIncompatible` and gets dropped, so the in-place legacy migrations become dead code. Delete the whole function (≈155 lines including the `legacyPreP2Schema` constant it owns). Also delete `legacyPreP2Schema` if nothing else references it (grep first).

```bash
grep -n "legacyPreP2Schema" internal/index/
# If only TestOpen_P2MigrationFromLegacyDB references it, remove both.
```

- [ ] **Step 3: Fix the direct-DDL insert in TestOpen_CascadeDeleteEdgesAndEmbeddings**

In `internal/index/index_test.go` find the block at the top of `TestOpen_CascadeDeleteEdgesAndEmbeddings`:

```go
if _, execErr := idx.DB().Exec(`
    INSERT INTO nodes (id, type, path, title, properties_json, last_mtime, last_size, last_checksum)
    VALUES ('src', 'note', 'src.md', 'src', '{}', 0, 0, 'h'),
           ('tgt', 'note', 'tgt.md', 'tgt', '{}', 0, 0, 'h');
`); execErr != nil {
    test.Fatalf("seed nodes: %v", execErr)
}
```

Replace with:

```go
if _, execErr := idx.DB().Exec(`
    INSERT INTO nodes (id, type, path, title, properties_json,
                       last_mtime, last_size, last_checksum,
                       kind, source)
    VALUES ('src', 'note', 'src.md', 'src', '{}', 0, 0, 'h', 'file', NULL),
           ('tgt', 'note', 'tgt.md', 'tgt', '{}', 0, 0, 'h', 'file', NULL);
`); execErr != nil {
    test.Fatalf("seed nodes: %v", execErr)
}
```

This still passes under the current loose DDL (the columns exist; explicit values are legal) and stays passing under the CHECK (file rows with NULL source and NULL parent_id satisfy the predicate).

- [ ] **Step 4: Swap `Upsert` → `BulkUpsert` for every sub-unit insertion**

Every test that constructs a `NodeRow` with `ParentID.Valid==true` and calls `repo.Upsert(row)` (or `nodeRepo.Upsert(...)`, `nodes.Upsert(...)`, `rt.Nodes.Upsert(...)`) must switch to `repo.BulkUpsert([]index.NodeRow{row})`. The known call sites:

```
internal/index/node_repo_test.go
  TestNodeRepo_ListByParent                                   (3 sub-units)
  TestNodeRepo_BulkDeleteCascadesEdgesAndEmbeddings           (1 sub-unit)
  TestNodeRepo_ListSubUnitsForFile_UnderscoreNoLeakage        (multiple sub-units across 2 parents)
  TestNodeRepo_ListSubUnitsForFiles                           (multiple sub-units across 2 parents)

internal/embed/drain_test.go
  TestDrainQueue_SubUnitEmbedsEmbedPayloadDirectly            (1 sub-unit, around line 859)
  TestDrainQueue_SubUnitWithEmptyEmbedPayloadIsSkipped        (likely 1 sub-unit)
  TestDrainQueue_MixedFileAndSubUnitBatch                     (mix; switch sub-unit inserts)

internal/mcp/tools_test.go
  TestTool_Query_SubUnitsIncludeUnitsAttachesMatchedUnits     (≥2 sub-units, around lines 1974/2042)
  TestTool_Query_DirectSubUnitFilterReturnsRowsWithParentID

internal/query/...
  TestQueryRun_StructuralIncludeUnitsAttachesSubUnits
  TestQueryRun_DirectSubUnitQueryReturnsRowsWithParentID
  TestQueryRun_SemanticSubUnitsGroupsByParent
```

Pattern transformation. Before:

```go
if upsertErr := repo.Upsert(index.NodeRow{
    ID: "doc#sub", Type: "paragraph", Path: "doc.md",
    PropertiesJSON: "{}", LastChecksum: "h",
    ParentID:     sql.NullString{String: "doc", Valid: true},
    Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
    EmbedPayload: sql.NullString{String: "body", Valid: true},
}); upsertErr != nil {
    test.Fatalf("Upsert sub-unit: %v", upsertErr)
}
```

After:

```go
if upsertErr := repo.BulkUpsert([]index.NodeRow{{
    ID: "doc#sub", Type: "paragraph", Path: "doc.md",
    PropertiesJSON: "{}", LastChecksum: "h",
    ParentID:     sql.NullString{String: "doc", Valid: true},
    Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
    EmbedPayload: sql.NullString{String: "body", Valid: true},
}}); upsertErr != nil {
    test.Fatalf("BulkUpsert sub-unit: %v", upsertErr)
}
```

When several sub-units share a parent, prefer one `BulkUpsert` call with all rows in the slice — matches the production sub-unit sync pipeline.

Grep template for finding remaining call sites in a file:

```bash
grep -n '\.Upsert(' <file> | xargs -I {} grep -B0 -A6 {}  # then eyeball for ParentID
```

- [ ] **Step 5: Swap `BulkUpsert` → `Upsert` for every file-row insertion**

Every test that calls `BulkUpsert([]NodeRow{...})` with rows whose `ParentID.Valid==false` must switch to a loop of `Upsert` calls (or a small local helper that does so). The known call sites:

```
internal/index/node_repo_test.go
  TestNodeRepo_BulkUpsert                  (2 file rows, lines 292–302)
  TestNodeRepo_BulkDelete                  (3 file rows seeded via BulkUpsert, lines 342–349)
```

`TestNodeRepo_BulkUpsert` is currently the only test that exercises `BulkUpsert` directly. Once `BulkUpsert` is strictly the sub-unit writer, this test should construct sub-unit-shaped rows (a parent file plus several sub-units) so the asserted behavior matches the writer's actual role. Rewrite the test body to:

1. `Upsert` a parent file row.
2. Construct 2–3 sub-unit `NodeRow`s with `ParentID` pointing at the parent and distinct ordinals.
3. `BulkUpsert` the slice.
4. Re-`BulkUpsert` with one row's `Title` changed; assert the update lands.
5. `BulkUpsert(nil)` is still a no-op assertion.

`TestNodeRepo_BulkDelete` only uses `BulkUpsert` as a seed; swap to a `for _, row := range rows { repo.Upsert(row) }` loop. The delete behavior under test is unchanged.

- [ ] **Step 6: Run the full suite under the loose DDL**

Run: `go test ./...`

Expected: clean. If any test still fails, that means a call site was missed — grep again with the templates above.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add internal/index/node_repo_test.go internal/index/index_test.go \
        internal/embed/drain_test.go internal/mcp/tools_test.go \
        internal/query
git commit -m "test(index): align direct-DDL inserts and writer choice with row kind"
```

Commit message body (the why):

```
Phase 2.5 promotes nodes.kind to NOT NULL and adds a CHECK that
ties kind/source/parent_id together. Several tests reach the
nodes table via the wrong writer (Upsert with parent_id set, or
BulkUpsert without it) or via direct INSERT statements missing
kind/source. Under the current loose DDL these tests happen to
work; under the CHECK they cannot. Fix every call site to use
the writer that matches the row's kind, fix the two direct-DDL
inserts, and delete two tests whose constructed states the CHECK
makes structurally impossible (TestCountSubUnitsUsesKindNotParentID,
TestOpen_P2MigrationFromLegacyDB — the latter exercises the dead
in-place migration path that the schema-bump rebuild model
retires per spec § "What the schema bump removes").

No production code change.
```

- [ ] **Step 9: Open the PR**

```bash
gh pr create --title "test(index): align direct-DDL inserts and writer choice with row kind" --body "$(cat <<'EOF'
## Summary
- Sub-unit rows now go through `BulkUpsert`; file rows go through `Upsert`. Test-only cleanup; no production code change.
- Direct-DDL `INSERT INTO nodes` in `TestOpen_CascadeDeleteEdgesAndEmbeddings` now includes `kind='file', source=NULL`.
- Deletes `TestCountSubUnitsUsesKindNotParentID` (Task 2.4 added it; Task 2.5b's CHECK makes its seed impossible — a strictly stronger guarantee).
- Deletes `TestOpen_P2MigrationFromLegacyDB` and the `legacyPreP2Schema` constant: per spec § *What the schema bump removes*, in-place legacy migration is retired in favor of `OpenOrRebuild`'s drop-and-reindex.
- Prereq for Phase 2, Task 5b (tighten DDL).

## Test plan
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- All four packages' tests pass under the current loose DDL.
- PR open and links Task 2.5b as its follow-up.

## Risks

- **Misidentified call site.** A test that constructs a sub-unit-shaped row but never touches the new writer might be missed by greps. Mitigation: Step 1's failure snapshot is authoritative — any test missing from the snapshot doesn't need attention from this PR.
- **A test depended on the legacy migration test running first** (e.g., shared package-level state). Verify with `go test -count=1 ./internal/index/...` after the deletion; Go's test isolation should make this a non-issue but worth a one-shot check.
