package index_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestNoLegacyMigrationsRemain is a compile-time guard against the
// in-place migration functions that became dead under the rebuild model.
// OpenOrRebuild handles every incompatible-schema scenario by dropping
// and rebuilding from the authoritative CREATE TABLE DDL, so any
// migrate* helper in index.go is unreachable and should not return.
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
		"migrateDropOrdinalColumn":    true,
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
