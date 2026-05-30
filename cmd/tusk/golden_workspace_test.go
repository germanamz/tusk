package main

import "testing"

// TestGoldenCLI_Workspace pins the workspace/indexing lifecycle: `init` (success
// + idempotency) and `reindex` across the default and a node-types config, where
// the same command emits a different summary depending on the manifest — the
// canonical (command × config) pattern the rest of the suite reuses.
func TestGoldenCLI_Workspace(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:       "init creates a workspace",
			noInit:     true,
			args:       []string{"init", "--name", "test"},
			wantStdout: "Initialized Tusk workspace at <WS>\n",
		},
		{
			name:       "init refuses an already-initialized workspace",
			args:       []string{"init", "--name", "test"},
			wantErr:    true,
			wantStderr: "init: tusk.toml already exists\n",
		},
		{
			name: "reindex indexes new files",
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "notes/a.md", goldenNoteA)
				writeFile(test, root, "notes/b.md", goldenNoteB)
			},
			args:       []string{"reindex"},
			wantStdout: "Reindex done: 2 indexed, 0 removed, 0 skipped\n",
		},
		{
			name: "reindex removes a node deleted on disk",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/a.md", goldenNoteA)
				removeFile(test, root, "notes/a.md")
			},
			args:       []string{"reindex"},
			wantStdout: "Reindex done: 0 indexed, 1 removed, 0 skipped\n",
		},
		{
			name: "reindex skips an off-schema file",
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "junk/loose.md", goldenNoTypeFile)
			},
			args:       []string{"reindex"},
			wantStdout: "Reindex done: 0 indexed, 0 removed, 1 skipped\n",
		},
		{
			// Same `reindex` command, schema config: a ticket missing its
			// required `summary` is indexed but flagged — the violation summary
			// differs from the clean run above purely because of [node-types].
			name:     "reindex flags a node-types violation",
			manifest: ticketSchemaManifest,
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "tickets/foo.md", goldenTicketNoSummary)
			},
			args:       []string{"reindex"},
			wantStdout: "Reindex done: 1 indexed, 0 removed, 0 skipped (1 property-violation)\nRun `tusk doctor` to inspect violations\n",
		},
	})
}

const (
	goldenNoteA           = "---\ntype: note\ntitle: A\n---\n\nAlpha.\n"
	goldenNoteB           = "---\ntype: note\ntitle: B\n---\n\nBeta.\n"
	goldenTicketNoSummary = "---\ntype: ticket\n---\n\nNo summary.\n"
	goldenNoTypeFile      = "Just loose markdown with no frontmatter type.\n"

	ticketSchemaManifest = `[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "summary", type = "string", required = true },
]
`
)
