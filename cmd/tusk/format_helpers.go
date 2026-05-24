package main

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
)

// outputFormat is the resolved render format for a read-verb command.
type outputFormat int

const (
	// formatLegacy means "no explicit format chosen, use the historical
	// table renderer". This preserves back-compat for unflagged callers
	// (existing scripts that pipe `tusk node list` into awk, etc).
	formatLegacy outputFormat = iota
	formatCompact
	formatJSON
)

// resolveFormat picks an output format given the user's flags. emitJSON is
// the legacy `--json` flag (sugar for `--format=json`); formatFlag is the
// new `--format` flag.
//
// Resolution rules:
//   - --json wins, always returns formatJSON.
//   - --format compact / json returns the matching format.
//   - empty --format with no --json defaults to:
//   - formatLegacy when no other shape-changing flags were set
//     (the caller passes hasShapeFlags=false), preserving back-compat
//   - formatCompact when stdout is a TTY (only relevant when the
//     caller asked for include/fields and is exploring at the terminal)
//   - formatJSON otherwise (piping into another tool)
func resolveFormat(emitJSON bool, formatFlag string, hasShapeFlags bool) (outputFormat, error) {
	if emitJSON {
		return formatJSON, nil
	}

	switch formatFlag {
	case "compact":
		return formatCompact, nil
	case "json":
		return formatJSON, nil
	case "":
		if !hasShapeFlags {
			return formatLegacy, nil
		}

		if isatty.IsTerminal(os.Stdout.Fd()) {
			return formatCompact, nil
		}

		return formatJSON, nil
	}

	return formatLegacy, fmt.Errorf("unknown --format %q (valid: compact, json)", formatFlag)
}
