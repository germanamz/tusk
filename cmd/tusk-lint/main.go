// Package main is the entrypoint for the tusk-lint custom linter binary.
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/germanamz/tusk/internal/lint/blankline"
	"github.com/germanamz/tusk/internal/lint/namederr"
	"github.com/germanamz/tusk/internal/lint/testhandle"
)

func main() {
	multichecker.Main(
		blankline.Analyzer,
		namederr.Analyzer,
		testhandle.Analyzer,
	)
}
