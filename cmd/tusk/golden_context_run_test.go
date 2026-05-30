package main

import "testing"

const (
	minimalManifest = "[workspace]\nname = \"test\"\n"

	aliasManifest = `[workspace]
name = "test"

[alias.snap]
command = "status"
description = "Quick snapshot"
`

	contextManifest = `[workspace]
name = "test"
sub-units = false

[context]
pinned = ["notes/alpha"]
`
)

// TestGoldenCLI_Run pins `tusk run`: the --list table, the empty-aliases line,
// the {alias,command,kind,result} dispatch envelope, and the unknown-alias error.
func TestGoldenCLI_Run(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:     "run --list shows declared aliases",
			manifest: aliasManifest,
			args:     []string{"run", "--list"},
			wantStdout: "snap  Quick snapshot\n" +
				"  → status\n",
		},
		{
			name:       "run --list with no aliases declared",
			manifest:   minimalManifest,
			args:       []string{"run", "--list"},
			wantStdout: "tusk run: no aliases declared in tusk.toml\n",
		},
		{
			name:     "run dispatches an alias as JSON",
			manifest: aliasManifest,
			args:     []string{"run", "snap", "--json"},
			// {alias,command,kind,result} envelope; last_reindex_at scrubbed.
			wantStdout: `{
  "alias": "snap",
  "command": "status",
  "kind": "status",
  "result": {
    "edge_count": 0,
    "embed_queue_depth": 0,
    "last_reindex_at":"<TS>",
    "nodes_by_type": {},
    "reindex_queue_depth": 0
  }
}
`,
		},
		{
			name:       "run rejects an unknown alias",
			manifest:   aliasManifest,
			args:       []string{"run", "nope"},
			wantErr:    true,
			wantStderr: "alias \"nope\" not declared in tusk.toml\n",
		},
	})
}

// TestGoldenCLI_Context pins `tusk context`: the no-[context] line and a compact
// digest composing pinned nodes + an alias include.
func TestGoldenCLI_Context(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:       "context with no [context] block",
			manifest:   minimalManifest,
			args:       []string{"context"},
			wantStdout: "tusk context: no [context] block declared in tusk.toml\n",
		},
		{
			name:     "context renders a pinned node",
			manifest: contextManifest,
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/alpha.md", goldenNoteA)
			},
			args: []string{"context"},
			wantStdout: "# Pinned\n" +
				"notes/alpha  note  A\n" +
				"  ---\n" +
				"  type: note\n" +
				"  title: A\n" +
				"  ---\n" +
				"  \n" +
				"  Alpha.\n",
		},
	})
}
