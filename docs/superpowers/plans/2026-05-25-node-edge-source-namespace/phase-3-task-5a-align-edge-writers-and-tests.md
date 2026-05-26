# Phase 3 — Task 5a: Align direct-DDL edge inserts and EdgeRow test fixtures with the future CHECK

**Phase:** 3 (Edges table reshape)
**Spec:** § *Edges table*, § *Index schema bump & rebuild*

**Goal:** Prepare the workspace for the CHECK constraint and NOT NULL `edges.kind` landing in Task 3.5. Today many tests construct `index.EdgeRow{...}` literals without setting `Kind`/`Source`, or seed edges through direct `INSERT INTO edges (...)` statements that omit `kind`. Under the loose Phase 3.1–3.4 DDL these tests work; under the tightened DDL they will not. Fix every literal to set `Kind: "direct"` (the writer path the test stands in for), fix the direct-DDL inserts, and delete the one test whose constructed pre-Phase-3 shape becomes impossible to bootstrap under tight DDL.

After this PR every test still passes under today's loose DDL; Task 3.5 can then promote the schema without further test churn.

## Inherits From

After Task 3.4:
- Frontmatter-direct edges writer sets `Kind='direct', Source=NULL` (PR #436).
- Ref-derived edges writer sets `Kind='derived', Source=NULL` (PR #434).
- Sub-unit structural edges writer sets `Kind='structural', Source='markdown'` (PR #438).
- Schema constant still leaves `edges.kind`/`edges.source` nullable; no CHECK constraint yet.

## Why a separate PR

Trial-applying Task 3.5's DDL surfaces failures in 8 packages (`cmd/tusk`, `internal/{aliasdispatch, doctor, graphexpand, index, mcp, query, subunit}`). Folding all of those test fixes into the DDL-tightening PR would (1) make a huge PR mixing schema work and test refactoring and (2) violate the lefthook pre-commit invariant (`make test`) at every intermediate commit. Landing the test corrections first under the still-loose DDL keeps each commit green and isolates the actual schema change to a small PR — same pattern as Phase 2.5a (PR #429) → Task 2.5b (PR #430).

## Files

Test-only changes. No production code touched.

- `cmd/tusk/cmd_doctor_test.go`
- `cmd/tusk/cmd_edge_remove_test.go`
- `internal/aliasdispatch/dispatch_test.go`
- `internal/doctor/doctor_test.go`
- `internal/graphexpand/walk_test.go`
- `internal/index/edge_repo_test.go`
- `internal/index/index_test.go` (deletes one test; fixes one direct DDL insert)
- `internal/index/node_repo_test.go`
- `internal/mcp/tools_test.go`
- `internal/query/expand_test.go`
- `internal/query/graphexpand_e2e_test.go`
- `internal/subunit/sync_test.go`

## The rule

The CHECK constraint that lands in Task 3.5 is:

```sql
CHECK (
    (kind IN ('direct', 'derived') AND source IS NULL) OR
    (kind = 'structural'           AND source IS NOT NULL)
)
```

Three writers each pin `kind`/`source` to one branch:

| Writer | Sets |
|---|---|
| Frontmatter direct (`subunit.collectFrontmatterEdges`) | `Kind='direct', Source=NULL` |
| Ref-derived (`refpolicy.Apply`) | `Kind='derived', Source=NULL` |
| Sub-unit structural (`subunit.Sync.rewriteContains`) | `Kind='structural', Source='markdown'` |

Every test that constructs an `EdgeRow` literal stands in for one of these writer paths. **Every existing test EdgeRow stands in for the direct path** (links/blocks/parent/references/tagged/mentions/parent/etc. — user-declared edge types). Setting `Kind: "direct"` on these literals matches the rule branch their `Source=NULL` implies and keeps them passing under both loose and tight DDL.

## Steps

- [ ] **Step 1: Snapshot the failure set under the future DDL**

To confirm we converge on the full list, temporarily apply Task 3.5's DDL change locally and capture the failing tests.

```bash
# Hand-edit internal/index/index.go to install the tight DDL (kind NOT NULL,
# UNIQUE(source, ...), CHECK, edges_source_type_idx, edges_kind_idx).
go test ./... 2>&1 | grep -E '^--- FAIL|^FAIL\s' | tee /tmp/check-failures.txt
git checkout -- internal/index/index.go
```

Expected `/tmp/check-failures.txt` (at the time this plan was written; verify on your machine):

```
--- FAIL: TestDoctor_AutoMigratesLegacyCLIRows
--- FAIL: TestDoctor_AutoMigratesLegacyMCPRows
--- FAIL: TestDoctor_AutoMigratesMixedCLIAndMCPRows
--- FAIL: TestDoctor_NoMigrateFlagSkipsMutation
--- FAIL: TestDoctor_AutoMigrateSkipsRowsWithMissingSourceFile
--- FAIL: TestDoctor_AutoMigrateIsIdempotent
--- FAIL: TestEdgeRemoveCmd_SweepsLegacyCLIRow
--- FAIL: TestDispatcher_EdgeList
--- FAIL: TestRun_FlagsDanglingEdges
--- FAIL: TestWalker_OneHop_ReferencesOnly
--- FAIL: TestWalker_OneHop_ReferencesAndContains
--- FAIL: TestWalker_TwoHop_References
--- FAIL: TestWalker_DeterministicOrdering
--- FAIL: TestWalker_UnknownEdgeTypeProducesNoRows
--- FAIL: TestWalker_ContextCancellation
--- FAIL: TestWalker_EmptySeeds
--- FAIL: TestWalker_DefensiveCopyEdgeTypes
--- FAIL: TestEdgeRepo_UpsertAllAndListBySource
--- FAIL: TestEdgeRepo_UpsertAllReplacesExistingEdgesForSource
--- FAIL: TestEdgeRepo_ListByTarget
--- FAIL: TestEdgeRepo_ListByType
--- FAIL: TestEdgeRepo_NeighborsByEdgeTypes
--- FAIL: TestOpen_DropsLegacyOrdinalColumn
--- FAIL: TestOpen_CascadeDeleteEdgesAndEmbeddings
--- FAIL: TestNodeRepo_BulkDeleteCascadesEdgesAndEmbeddings
--- FAIL: TestTool_EdgeList
--- FAIL: TestEdgeRemoveMCP_SweepsLegacyMCPRow
--- FAIL: TestDoctorMCP_AutoMigratesLegacyRows
--- FAIL: TestDoctorMCP_NoMigrateReportsLegacyRowsAsDrift
--- FAIL: TestTool_EdgeList_FormatCompact
--- FAIL: TestExpandListRows_Edges
--- FAIL: TestQueryRun_GraphExpansion_PromotesReferentialHub
--- FAIL: TestQueryRun_GraphExpansion_LiftsNonSeedIntoTopK
--- FAIL: TestSync_FrontmatterEdgesSurviveContainsRewrite
```

- [ ] **Step 2: Delete `TestOpen_DropsLegacyOrdinalColumn`**

The test (in `internal/index/index_test.go`) seeds a pre-Phase-3 legacy edges schema (no `kind`/`source` columns), then calls `index.Open` to exercise `migrateDropOrdinalColumn`. Under tight DDL the bootstrap `CREATE INDEX edges_kind_idx ON edges(kind)` runs *before* the migration and fails on the legacy column-less table. Per spec § *What the schema bump removes*, the rebuild model retires this in-place migration path: any DB lacking the current `schema_version` key trips `ErrSchemaIncompatible` and gets dropped via `OpenOrRebuild`, so `migrateDropOrdinalColumn` becomes dead code. Phase 6.2 removes the function itself; the test goes now because tight DDL makes it unrunnable. Same precedent as Phase 2.5a deleting `TestOpen_P2MigrationFromLegacyDB`.

- [ ] **Step 3: Fix the direct-DDL insert in `TestOpen_CascadeDeleteEdgesAndEmbeddings`**

In `internal/index/index_test.go`:

```go
INSERT INTO edges (type, source_id, target_id, source_path)
VALUES ('links', 'src', 'tgt', 'src.md');
```

Replace with:

```go
INSERT INTO edges (type, source_id, target_id, source_path, kind)
VALUES ('links', 'src', 'tgt', 'src.md', 'direct');
```

Passes under loose DDL (column exists; value legal) and tight DDL (`kind='direct'` + `source=NULL` satisfies the CHECK).

- [ ] **Step 4: Add `Kind: "direct"` to every `EdgeRow{...}` literal**

Touch every call site found by:

```bash
grep -rn 'index\.EdgeRow{' --include='*_test.go' .
```

Every existing literal stands in for the direct writer path (user-declared edge types: `links`, `blocks`, `parent`, `references`, `tagged`, `mentions`, etc.) — none uses `Source`. The mechanical edit per literal is to insert `Kind: "direct",` alongside the existing fields. Example:

Before:
```go
{Type: "blocks", SourceID: "a", TargetID: "x", SourcePath: "a.md"}
```

After:
```go
{Type: "blocks", SourceID: "a", TargetID: "x", SourcePath: "a.md", Kind: "direct"}
```

If a test constructs an EdgeRow that should logically be a different kind (e.g. a `contains` edge that stands in for the structural writer), align it with that writer's pinned kind/source — but a grep of the current literals shows all of them are direct-style.

- [ ] **Step 5: Run the full suite under loose DDL**

Run: `go test ./...`

Expected: clean. If any test still fails, that means a call site was missed — grep again with the templates above.

- [ ] **Step 6: Re-verify under tight DDL**

Temporarily apply Task 3.5's DDL change again and run `go test ./...`. Expected: clean (every snapshotted failure now passes). Revert before committing.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add cmd/tusk/cmd_doctor_test.go cmd/tusk/cmd_edge_remove_test.go \
        internal/aliasdispatch/dispatch_test.go \
        internal/doctor/doctor_test.go \
        internal/graphexpand/walk_test.go \
        internal/index/edge_repo_test.go internal/index/index_test.go \
        internal/index/node_repo_test.go \
        internal/mcp/tools_test.go \
        internal/query/expand_test.go internal/query/graphexpand_e2e_test.go \
        internal/subunit/sync_test.go
git commit -m "test(index): align edge test fixtures with future kind/source CHECK"
```

Commit message body (the why):

```
Phase 3.5 promotes edges.kind to NOT NULL and adds a CHECK that ties
kind/source to the writer path. Every test that constructs an EdgeRow
literal (or seeds via direct DDL INSERT INTO edges) currently omits
kind. Under the loose DDL those rows insert as kind=NULL and tests
happen to work; under the CHECK they cannot. Set Kind: "direct" on
every literal (each stands in for the direct writer path), add kind to
the one direct-DDL insert in TestOpen_CascadeDeleteEdgesAndEmbeddings,
and delete TestOpen_DropsLegacyOrdinalColumn — its pre-Phase-3 seed
becomes impossible to bootstrap under the tight DDL, and the migration
it exercises is retired by the schema-bump rebuild model (spec §
"What the schema bump removes"; Phase 6.2 removes the function).

No production code change.
```

- [ ] **Step 9: Open the PR**

```bash
gh pr create --title "test(index): align edge test fixtures with future kind/source CHECK" --body "$(cat <<'EOF'
## Summary
- Sets Kind: "direct" on every EdgeRow literal in tests. They all stand in for the frontmatter-direct writer path; under the future CHECK constraint the implicit kind=NULL is rejected.
- Adds `kind` column to the direct-DDL `INSERT INTO edges` in `TestOpen_CascadeDeleteEdgesAndEmbeddings`.
- Deletes `TestOpen_DropsLegacyOrdinalColumn`: its pre-Phase-3 legacy edges schema (no kind/source) cannot bootstrap under the future tight DDL, and the migration it covers is retired by the schema-bump rebuild model — Phase 6.2 removes the function.
- Test-only; no production change. Prereq for Phase 3, Task 5 (tighten edges DDL).

## Test plan
- [ ] \`go test ./...\` passes
- [ ] \`make vet && make lint\` clean
EOF
)"
```

## Done when

- All eight packages' tests pass under both loose and tight DDL.
- PR open and links Task 3.5 as its follow-up.

## Risks

- **Missed call site.** A test that constructs an EdgeRow but never appears in the failure snapshot might be missed by greps. Mitigation: Step 1's snapshot is authoritative — any test missing from it doesn't need attention here.
- **A literal that should be `derived` or `structural`.** Current grep shows none; if Step 6's re-verification surfaces a CHECK failure on a literal we set to `direct`, that literal is constructing a row for a different writer path and needs the matching kind/source.
