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

// TestGoldenCLI_NodeLifecycle pins the create/modify/move/delete/get-json verbs
// across the permissive default and a node-types schema (the human-readable
// rejection message that contrasts with the MCP structured-JSON form).
func TestGoldenCLI_NodeLifecycle(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:       "node create writes a node (permissive)",
			args:       []string{"node", "create", "--type", "note", "--path", "notes/x.md", "--title", "X"},
			wantStdout: "Created notes/x.md (id=notes/x)\n",
		},
		{
			name:     "node create rejects a missing required property",
			manifest: ticketSchemaManifest,
			args:     []string{"node", "create", "--type", "ticket", "--path", "tickets/foo.md"},
			wantErr:  true,
			// Human-readable form — contrast with the MCP structured-JSON
			// rejection (goldenNodeTypesRejection) for the same failure.
			wantStderr: `node-types: rejected create: ticket "tickets/foo" has 1 error:
  - property "summary" is required (declared in [node-types.ticket])
`,
		},
		{
			name: "node modify sets a property",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/m.md", goldenNoteA)
			},
			args:       []string{"node", "modify", "notes/m", "--prop", "tag=urgent"},
			wantStdout: "Modified notes/m\n",
		},
		{
			name: "node move renames and reports referrers",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/old.md", goldenNoteA)
			},
			args:       []string{"node", "move", "notes/old", "notes/new"},
			wantStdout: "Renamed notes/old → notes/new (rewrote 0 referring file(s))\n",
		},
		{
			name: "node delete removes a node",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/d.md", goldenNoteA)
			},
			args:       []string{"node", "delete", "notes/d"},
			wantStdout: "Deleted notes/d\n",
		},
		{
			name: "node get --json emits the structured envelope",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/j.md", goldenNoteA)
			},
			args: []string{"node", "get", "notes/j", "--json"},
			// Pretty MarshalIndent, sorted keys; with no --include the full node
			// is emitted (body + null edges + properties incl. title/type).
			wantStdout: `{
  "body": "Alpha.\n",
  "edges": null,
  "id": "notes/j",
  "path": "notes/j.md",
  "properties": {
    "title": "A",
    "type": "note"
  },
  "title": "A",
  "type": "note"
}
`,
		},
	})
}

// TestGoldenCLI_NodeRender pins the plain-text render output for a markdown node
// (markup stripped) and an HTML node (tags stripped, entities decoded). Both
// fixtures are written and reindexed via writeGoldenNode so their rows exist
// before render resolves them.
func TestGoldenCLI_NodeRender(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name: "node render strips markdown markup",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/r.md", goldenRenderMarkdownFile)
			},
			args:       []string{"node", "render", "notes/r"},
			wantStdout: goldenRenderMarkdownPlain,
		},
		{
			name: "node render strips html tags",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "page.html", goldenRenderHTMLFile)
			},
			args:       []string{"node", "render", "page.html"},
			wantStdout: goldenRenderHTMLPlain,
		},
	})
}

// goldenRenderMarkdownFile is the on-disk markdown fixture for the render golden.
const goldenRenderMarkdownFile = "---\ntype: note\ntitle: R\n---\n\n# Heading\n\nFirst **bold** line.\n"

// goldenRenderMarkdownPlain is the exact expected plain-text render of the
// markdown fixture (markup removed, trailing newline from Fprintln).
const goldenRenderMarkdownPlain = "Heading\n\nFirst bold line.\n"

// goldenRenderHTMLFile is the on-disk HTML fixture for the render golden.
const goldenRenderHTMLFile = "<html><head><meta name=\"tusk:type\" content=\"note\"></head>" +
	"<body><h1>Greeting</h1><p>Hello &amp; goodbye.</p></body></html>"

// goldenRenderHTMLPlain is the exact expected plain-text render of the HTML
// fixture (tags stripped, &amp; decoded, trailing newline from Fprintln).
const goldenRenderHTMLPlain = "Greeting\n\nHello & goodbye.\n"
