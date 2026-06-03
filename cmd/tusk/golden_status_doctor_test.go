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

// TestGoldenCLI_DoctorWorkflow pins `tusk doctor` for a workflow-governed
// workspace — the #497 regression guard at the report-rendering level. A node
// in a legally-reached non-initial state must report no issues; a status that
// is not a declared state must surface a workflow-violation rendered with the
// real cause (the validator's full message, including the declared-states list)
// rather than the old hardcoded "is not a declared state" reconstruction.
func TestGoldenCLI_DoctorWorkflow(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:     "doctor is clean for a legally-reached non-initial state",
			manifest: kanbanWorkflowManifest,
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "tickets/demo.md", goldenTicketActive)
			},
			args:       []string{"doctor"},
			wantStdout: "doctor: no issues\n" + goldenDoctorWorkflowTail,
		},
		{
			name:     "doctor surfaces an undeclared status with the real cause",
			manifest: kanbanWorkflowManifest,
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "tickets/demo.md", goldenTicketBogus)
			},
			args: []string{"doctor"},
			// The message is the validator's full rendered Error — note the
			// "declared states:" continuation line, which the old hardcoded
			// doctor string (#497) could not produce.
			wantStdout: "  [workflow-violation] tickets/demo: workflow \"kanban\": \"bogus\" is not a declared state for property \"status\"\n" +
				"  declared states: active, completed, pending\n" +
				goldenDoctorWorkflowTail,
		},
	})
}

// goldenDoctorWorkflowTail is the deterministic remainder of `tusk doctor` for
// the kanbanWorkflowManifest workspace (sub-units disabled, so no sub-unit
// pane): the queue depths plus the graph-expansion pane. The single ticket node
// leaves one file-level embed job queued. Shared by the clean and violation
// cases, which differ only in the leading issue lines.
const goldenDoctorWorkflowTail = "embed queue depth: 1\n" +
	"reindex queue depth: 0\n" +
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
	"    contains (not declared in manifest; walker will skip it)\n" +
	"    parent (not declared in manifest; walker will skip it)\n" +
	"    references (not declared in manifest; walker will skip it)\n" +
	"    tagged (not declared in manifest; walker will skip it)\n" +
	"  hint: use `tusk query --semantic ... --explain` to see per-result graph/cosine breakdown when debugging.\n"

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
	"  deduped sub-units     0\n" +
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
