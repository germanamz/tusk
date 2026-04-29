// Package blankline implements a Go analyzer that enforces rule 2 from STYLE.md:
// blank lines must appear before and after if-err guards.
package blankline

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "blankline",
	Doc:  "checks blank lines around if err != nil guards (rule 2 from STYLE.md)",
	Run:  run,
}

// isErrName reports whether name is an error identifier: exactly "err" or
// any identifier that ends with "Err" (capital E, lowercase r).
func isErrName(name string) bool {
	return name == "err" || strings.HasSuffix(name, "Err")
}

// errLHSName returns the name of the leftmost LHS identifier that looks like
// an error variable, or "" if none.
func errLHSName(stmt *ast.AssignStmt) string {
	for _, expr := range stmt.Lhs {
		ident, ok := expr.(*ast.Ident)
		if !ok {
			continue
		}
		if isErrName(ident.Name) {
			return ident.Name
		}
	}
	return ""
}

// ifGuardErrName returns the name of the error identifier checked by the
// if-statement's condition if the condition has the form `<errIdent> != nil`,
// otherwise returns "".
func ifGuardErrName(ifStmt *ast.IfStmt) string {
	// Accept only bare conditions (no init stmt) of the form `X != nil`.
	if ifStmt.Init != nil {
		return ""
	}
	bin, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return ""
	}
	xIdent, xOk := bin.X.(*ast.Ident)
	yIdent, yOk := bin.Y.(*ast.Ident)
	if !xOk || !yOk {
		return ""
	}
	if yIdent.Name != "nil" {
		return ""
	}
	if isErrName(xIdent.Name) {
		return xIdent.Name
	}
	return ""
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			block, ok := node.(*ast.BlockStmt)
			if !ok {
				return true
			}
			stmts := block.List
			for idx, stmt := range stmts {
				assign, ok := stmt.(*ast.AssignStmt)
				if !ok {
					continue
				}
				errName := errLHSName(assign)
				if errName == "" {
					continue
				}
				// Look for the immediately following if-guard in the same block.
				if idx+1 >= len(stmts) {
					continue
				}
				ifStmt, ok := stmts[idx+1].(*ast.IfStmt)
				if !ok {
					continue
				}
				guardName := ifGuardErrName(ifStmt)
				if guardName != errName {
					continue
				}

				// Check blank line BEFORE the if-guard (between assign and if).
				assignEnd := pass.Fset.Position(assign.End()).Line
				ifStart := pass.Fset.Position(ifStmt.Pos()).Line
				if ifStart-assignEnd < 2 {
					pass.Reportf(assign.Pos(), "blankline: missing blank line before if-err guard")
				}

				// Check blank line AFTER the if-guard (between if closing brace and next stmt).
				// Only when there is a next statement.
				if idx+2 < len(stmts) {
					ifEnd := pass.Fset.Position(ifStmt.End()).Line
					nextStart := pass.Fset.Position(stmts[idx+2].Pos()).Line
					if nextStart-ifEnd < 2 {
						pass.Reportf(ifStmt.Pos(), "blankline: missing blank line after if-err guard")
					}
				}
			}
			return true
		})
	}
	return nil, nil
}
