package main

import "testing"

// TestGoldenCLI_Node pins the byte-stable behavior of the `tusk node *` command
// family. More cases (create/list/modify/move/delete across permissive and
// node-types/workflow configs) land here as the matrix is built out.
func TestGoldenCLI_Node(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			// The highest-risk trap on the CLI side: `node get` with no shape
			// flags must echo the markdown file verbatim, byte-for-byte. The
			// fixture is written directly (not via `node create`) so the test
			// owns the exact on-disk bytes the passthrough is asserted against.
			name: "node get raw passthrough echoes the file verbatim",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/hello.md", goldenHelloFile)
			},
			args:       []string{"node", "get", "notes/hello"},
			wantStdout: goldenHelloFile,
		},
	})
}

// goldenHelloFile is the exact on-disk content of the node-get fixture. Because
// `node get` (no flags) reads the file and prints it verbatim, this literal is
// simultaneously the fixture and the expected stdout.
const goldenHelloFile = "---\ntype: note\ntitle: Hello\n---\n\n# Hello\n\nFirst body line.\n"
