package main

import "testing"

// TestGoldenCLI_Status pins `tusk status`: the sorted TYPE/COUNT tabwriter block
// plus the edges / queue-depth / last-reindex summary (the timestamp scrubbed).
func TestGoldenCLI_Status(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name: "status on an empty workspace",
			args: []string{"status"},
			wantStdout: "TYPE  COUNT\n" +
				"edges: 0\n" +
				"embed queue depth: 0\n" +
				"reindex queue depth: 0\n" +
				"last reindex (unix ns): \n",
		},
		{
			name: "status counts nodes by type",
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "notes/a.md", goldenNoteA)
				writeFile(test, root, "notes/b.md", goldenNoteB)
				writeFile(test, root, "tasks/t.md", goldenTaskT)
				reindexWorkspace(test, root)
			},
			args: []string{"status"},
			// Default workspace has sub-units enabled, so node bodies become
			// `paragraph` sub-units (3) with `contains` edges (3) and embed jobs.
			wantStdout: "TYPE       COUNT\n" +
				"note       2\n" +
				"paragraph  3\n" +
				"task       1\n" +
				"edges: 3\n" +
				"embed queue depth: 6\n" +
				"reindex queue depth: 0\n" +
				"last reindex (unix ns): <TS>\n",
		},
	})
}

// TestGoldenCLI_Doctor pins `tusk doctor`: the clean report ("doctor: no issues")
// plus the queue depths and the sub-unit / graph-expansion panes — exit 0.
func TestGoldenCLI_Doctor(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:       "doctor on a clean workspace reports no issues",
			args:       []string{"doctor"},
			wantStdout: goldenDoctorClean,
		},
	})
}

const goldenTaskT = "---\ntype: task\ntitle: T\n---\n\nTask body.\n"

// goldenDoctorClean is the exact `tusk doctor` output for a freshly-init'd
// workspace: no issues, zeroed queue depths, the all-zero sub-unit pane, and the
// graph-expansion pane reflecting the default manifest's [query.graph-expansion]
// block (the default-pack edge types, minus those not declared as [edge-types]).
const goldenDoctorClean = "doctor: no issues\n" +
	"embed queue depth: 0\n" +
	"reindex queue depth: 0\n" +
	"sub-units:\n" +
	"  total                 0\n" +
	"  hash collisions       0\n" +
	"  orphans               0\n" +
	"  queue (files)         0\n" +
	"  queue (sub-units)     0\n" +
	"  oversize payloads     0\n" +
	"  reserved conflicts    0\n" +
	"graph expansion:\n" +
	"  enabled               false\n" +
	"  hops                  1\n" +
	"  weight                0.20\n" +
	"  candidate multiplier  5\n" +
	"  edge types:\n" +
	"    references\n" +
	"    parent\n" +
	"    tagged\n" +
	"    contains\n" +
	"  unknown edge types:\n" +
	"    parent (not declared in manifest; walker will skip it)\n" +
	"    references (not declared in manifest; walker will skip it)\n" +
	"    tagged (not declared in manifest; walker will skip it)\n" +
	"  hint: use `tusk query --semantic ... --explain` to see per-result graph/cosine breakdown when debugging.\n"
