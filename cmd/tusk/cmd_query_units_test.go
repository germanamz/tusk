package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const ledgerManifest = `
[workspace]
name = "test"

[node-types.note]
properties = []
`

// ledgerNote is the #765 repro file. Its sections sit at lines 5-13 (Ledger),
// 7-9 (Entries) and 11-13 (Balances), counting the frontmatter.
const ledgerNote = "---\ntype: note\n---\n\n# Ledger\n\n## Entries\n\nThe ledger stores one row per entry.\n\n## Balances\n\nBalances are derived from entries, never stored.\n"

func queryUnitsJSON(test *testing.T, args ...string) []map[string]any {
	test.Helper()

	out := &bytes.Buffer{}

	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(append([]string{"query"}, args...))

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("query %v: %v\n%s", args, execErr, out.String())
	}

	var rows []map[string]any

	if decodeErr := json.Unmarshal(out.Bytes(), &rows); decodeErr != nil {
		test.Fatalf("decode %q: %v", out.String(), decodeErr)
	}

	return rows
}

// TestQueryCmd_IncludeUnitsCarriesLinesAndHeadings drives the full path from
// a file on disk: parse, reindex, query. Lines count the frontmatter, sections
// carry their heading, and --max-units cuts the outline with units_total
// reporting the full count.
func TestQueryCmd_IncludeUnitsCarriesLinesAndHeadings(test *testing.T) {
	root := initWorkspaceWithManifest(test, ledgerManifest)

	writeNodeFile(test, root, "docs/ledger.md", ledgerNote)

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	rows := queryUnitsJSON(test, "type=note", "--include", "units", "--max-units", "2", "--format", "json")

	if len(rows) != 1 {
		test.Fatalf("rows = %v, want 1", rows)
	}

	units, _ := rows[0]["matched_units"].([]any)

	if len(units) != 2 {
		test.Fatalf("matched_units = %v, want 2", units)
	}

	if total, _ := rows[0]["units_total"].(float64); total != 5 {
		test.Errorf("units_total = %v, want 5", rows[0]["units_total"])
	}

	want := []struct {
		heading string
		lines   string
	}{{"Ledger", "[5,13]"}, {"Entries", "[7,9]"}}

	for index, unit := range units {
		fields := unit.(map[string]any)
		lines, _ := json.Marshal(fields["lines"])

		if fields["heading"] != want[index].heading || string(lines) != want[index].lines {
			test.Errorf("unit %d = heading %v lines %s, want %s %s", index, fields["heading"], lines, want[index].heading, want[index].lines)
		}
	}
}

func TestQueryCmd_RejectsNegativeMaxUnits(test *testing.T) {
	initWorkspace(test)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"query", "type=note", "--max-units", "-1"})

	execErr := cmd.Execute()

	if execErr == nil || !strings.Contains(execErr.Error(), "max-units") {
		test.Errorf("error = %v, want a max-units error", execErr)
	}
}
