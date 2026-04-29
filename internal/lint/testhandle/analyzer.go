// Package testhandle implements a Go analyzer that enforces rule 4 from STYLE.md:
// testing-handle parameters must use standardized names — *testing.T → "test",
// *testing.B → "bench", testing.TB → "harness".
package testhandle

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/germanamz/tusk/internal/lint/pathfilter"
)

// Analyzer is the exported analysis.Analyzer for the testhandle rule.
var Analyzer = &analysis.Analyzer{
	Name: "testhandle",
	Doc:  "checks that testing-handle parameters use standardized names (rule 4 from STYLE.md)",
	Run:  run,
}

// requiredNames maps a type string (as written in source) to its required parameter name.
var requiredNames = map[string]string{
	"*testing.T": "test",
	"*testing.B": "bench",
	"testing.TB": "harness",
}

// typeString returns the canonical type string for a type expression, or ""
// if the expression does not match a testing handle type.
//
// Recognised forms:
//   - *ast.StarExpr { X: *ast.SelectorExpr { X: "testing", Sel: "T"|"B" } }
//   - *ast.SelectorExpr { X: "testing", Sel: "TB" }
func typeString(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.StarExpr:
		sel, ok := node.X.(*ast.SelectorExpr)
		if !ok {
			return ""
		}

		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok || pkgIdent.Name != "testing" {
			return ""
		}

		name := sel.Sel.Name
		if name != "T" && name != "B" {
			return ""
		}

		return fmt.Sprintf("*testing.%s", name)

	case *ast.SelectorExpr:
		pkgIdent, ok := node.X.(*ast.Ident)
		if !ok || pkgIdent.Name != "testing" {
			return ""
		}

		if node.Sel.Name != "TB" {
			return ""
		}

		return "testing.TB"
	}

	return ""
}

// checkFields emits diagnostics for any parameter in fields whose name
// violates the requiredNames table.
func checkFields(pass *analysis.Pass, fields []*ast.Field) {
	for _, field := range fields {
		typeStr := typeString(field.Type)
		if typeStr == "" {
			continue
		}

		required, ok := requiredNames[typeStr]
		if !ok {
			continue
		}

		for _, ident := range field.Names {
			if ident.Name == "_" {
				continue
			}

			if ident.Name != required {
				pass.Reportf(
					ident.Pos(),
					"testhandle: parameter of type %s must be named %q, got %q",
					typeStr,
					required,
					ident.Name,
				)
			}
		}
	}
}

func run(pass *analysis.Pass) (any, error) {
	if pathfilter.Excluded(pass.Pkg.Path()) {
		return nil, nil
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.FuncType)
			if !ok {
				return true
			}

			if decl.Params != nil {
				checkFields(pass, decl.Params.List)
			}

			return true
		})
	}

	return nil, nil
}
