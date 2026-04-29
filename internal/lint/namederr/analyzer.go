// Package namederr implements a Go analyzer that enforces rule 3 from STYLE.md:
// when a lexical block contains two or more `:=` assignments that declare `err`,
// all of them must be renamed to typed names (e.g. fooErr, barErr).
package namederr

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/germanamz/tusk/internal/lint/pathfilter"
)

// Analyzer is the exported analysis.Analyzer for the namederr rule.
var Analyzer = &analysis.Analyzer{
	Name: "namederr",
	Doc:  "checks that 'err' is not declared via := more than once in the same lexical block (rule 3 from STYLE.md)",
	Run:  run,
}

// errDefAssignments returns the slice of *ast.AssignStmt within a single
// *ast.BlockStmt that declare a variable literally named "err" via :=.
// It looks only at the direct children of the block (no recursion — callers
// handle nesting by visiting every block independently via ast.Inspect).
func errDefAssignments(block *ast.BlockStmt) []*ast.AssignStmt {
	var found []*ast.AssignStmt

	for _, stmt := range block.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}

		if assign.Tok != token.DEFINE {
			continue
		}

		for _, lhs := range assign.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if ok && ident.Name == "err" {
				found = append(found, assign)
				break
			}
		}
	}

	return found
}

func run(pass *analysis.Pass) (any, error) {
	if pathfilter.Excluded(pass.Pkg.Path()) {
		return nil, nil
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			block, ok := node.(*ast.BlockStmt)
			if !ok {
				return true
			}

			assignments := errDefAssignments(block)
			count := len(assignments)

			if count < 2 {
				return true
			}

			msg := fmt.Sprintf(
				"namederr: 'err' is shadowed %d times in this scope; rename all instances to typed names (e.g. fooErr, barErr)",
				count,
			)

			for _, assign := range assignments {
				pass.Reportf(assign.Pos(), "%s", msg)
			}

			return true
		})
	}

	return nil, nil
}
