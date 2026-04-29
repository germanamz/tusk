// Package main is the entrypoint for the tusk-lint custom linter binary.
package main

import "golang.org/x/tools/go/analysis/multichecker"

func main() {
	// Analyzers registered in Phase 2 of the v0.14 milestone. (bridge code — removed by P2-T5)
	multichecker.Main()
}
