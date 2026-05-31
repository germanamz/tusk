package main

import "testing"

// TestGoldenCLI_ReindexWorkflow pins the `tusk reindex` summary line for a
// workspace governed by a workflow pack — the regression guard for #497, where
// reindex wrongly flagged a node sitting in a legally-reached non-initial state
// as a workflow-violation on every pass. A node in a declared non-initial state
// must reindex cleanly; only a status that is not a declared state at all is
// still counted as drift.
func TestGoldenCLI_ReindexWorkflow(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:     "reindex does not flag a legally-reached non-initial state",
			manifest: kanbanWorkflowManifest,
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "tickets/demo.md", goldenTicketActive)
			},
			args:       []string{"reindex"},
			wantStdout: "Reindex done: 1 indexed, 0 removed, 0 skipped\n",
		},
		{
			name:     "reindex flags a status that is not a declared state",
			manifest: kanbanWorkflowManifest,
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "tickets/demo.md", goldenTicketBogus)
			},
			args: []string{"reindex"},
			wantStdout: "Reindex done: 1 indexed, 0 removed, 0 skipped (1 workflow-violation)\n" +
				"Run `tusk doctor` to inspect violations\n",
		},
	})
}

// kanbanWorkflowManifest is the inline equivalent of `pack add kanban`: a
// three-state workflow over the `ticket` type. Sub-units are disabled so the
// reindex/doctor output reflects only the ticket node itself (no paragraph
// sub-units or contains edges).
const kanbanWorkflowManifest = `[workspace]
name = "test"
sub-units = false

[behaviors.workflow.kanban]
applies-to = ["ticket"]
states = [
    { name = "pending", initial = true },
    { name = "active", start = true },
    { name = "completed", terminal = true, done = true },
]
transitions = [
    { from = "pending", to = "active" },
    { from = "active", to = "completed" },
    { from = "active", to = "pending" },
    { from = "completed", to = "pending" },
]
`

// goldenTicketActive is a ticket sitting in a declared, non-initial state — the
// exact shape that #497 wrongly flagged on reindex.
const goldenTicketActive = "---\ntype: ticket\nstatus: active\n---\n\nDemo ticket.\n"

// goldenTicketBogus is a ticket whose status is not a declared state at all —
// genuine drift that reindex must still surface.
const goldenTicketBogus = "---\ntype: ticket\nstatus: bogus\n---\n\nDemo ticket.\n"
