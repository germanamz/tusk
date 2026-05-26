# Phase 6 — Task 2: Remove dead legacy migration code

**Phase:** 6 (Cleanup)
**Spec:** § *What the schema bump removes*

**Goal:** Delete the in-place migration functions in `internal/index/index.go` that became dead under the rebuild model: `migrateRelaxNodesPathUnique`, `migrateAddSubUnitColumns`, `migrateEmbeddingsPrimaryKey`, `migrateAddEdgesSourceFK`, and any helper used exclusively by them.

## Inherits From

After Phase 6 Task 1 (embeddings uniqueness fix):
- `OpenOrRebuild` handles every incompatible-schema scenario by dropping and rebuilding.
- The embeddings table now stores one row per `(node_id, chunk_idx)`; the migration that ratcheted toward the now-superseded shape (`migrateEmbeddingsPrimaryKey`) is no longer the authoritative path for any index.
- The schema_version mechanism guarantees no caller hits the legacy migration paths.

## Files

- **Modify:** `internal/index/index.go`
- **Modify or delete:** any helper file under `internal/index/` exclusive to the migration functions.
- **Delete:** any test file dedicated to in-place migration behavior.

## Steps

- [ ] **Step 1: Enumerate the dead code**

Run:
```
grep -n 'func migrate' internal/index/index.go
```

Identify each migration function. Verify it is unreachable by tracing its callers:
```
grep -rn 'migrateRelaxNodesPathUnique\|migrateAddSubUnitColumns\|migrateEmbeddingsPrimaryKey\|migrateAddEdgesSourceFK' internal/
```

If the only callers are within `Open`'s migration loop, the function is dead.

- [ ] **Step 2: Write a failing test asserting the functions are gone**

This task's "test" is a compile-time check via grep. Add a small post-removal grep script as a CI hint and/or write a `TestNoLegacyMigrationsRemain`:

```go
package index_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestNoLegacyMigrationsRemain(test *testing.T) {
	test.Parallel()

	fset := token.NewFileSet()
	parsed, parseErr := parser.ParseFile(fset, "index.go", nil, parser.SkipObjectResolution)
	if parseErr != nil {
		test.Fatalf("parse: %v", parseErr)
	}

	forbidden := map[string]bool{
		"migrateRelaxNodesPathUnique": true,
		"migrateAddSubUnitColumns":    true,
		"migrateEmbeddingsPrimaryKey": true,
		"migrateAddEdgesSourceFK":     true,
	}

	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if forbidden[fn.Name.Name] {
			test.Errorf("legacy migration %q still present in index.go", fn.Name.Name)
		}
		if strings.HasPrefix(fn.Name.Name, "migrate") {
			test.Logf("note: %q still present — confirm it is still required", fn.Name.Name)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/index/... -run TestNoLegacyMigrationsRemain -v`

Expected: FAIL — the functions still exist.

- [ ] **Step 4: Remove the dead functions**

Delete the four named functions and any unreferenced helpers they depended on. Remove their call sites in `Open` (they should already be no-ops if the schema_version contract is honored, but the loop entries still exist).

After removal, `Open` should:

1. Run `CREATE TABLE IF NOT EXISTS` DDL for every table.
2. Read `meta.schema_version`.
3. Compare against `SchemaVersion`; mismatch → return `ErrSchemaIncompatible`.

No additional migration steps. The DDL constant in `internal/index/index.go` is authoritative.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/index/... -run TestNoLegacyMigrationsRemain -v`

Expected: PASS.

- [ ] **Step 6: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/index
git commit -m "chore(cleanup): remove dead legacy migration code"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "chore(cleanup): remove dead legacy migration code" --body "$(cat <<'EOF'
## Summary
- Removes `migrateRelaxNodesPathUnique`, `migrateAddSubUnitColumns`, `migrateEmbeddingsPrimaryKey`, `migrateAddEdgesSourceFK` and exclusive helpers
- These became dead code under the rebuild model: every incompatible-schema index is dropped by `OpenOrRebuild` before they could run
- Phase 6 (cleanup) of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes
- [ ] AST-based legacy-migration check passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Post-Implementation Review

After this PR merges (the final task in Phase 6), the planning agent runs the post-implementation review against the full spec. If everything checks out, the final commit removes the entire `docs/superpowers/plans/2026-05-25-node-edge-source-namespace/` directory:

```
git rm -r docs/superpowers/plans/2026-05-25-node-edge-source-namespace/
git commit -m "chore(cleanup): remove node-edge source-namespace plan docs"
```

The spec at `docs/superpowers/specs/2026-05-25-node-edge-source-namespace-design.md` stays in place as durable architectural reference.

## Done when

- Legacy migrations removed.
- AST test passes.
- Workspace suite green.
- PR open.
