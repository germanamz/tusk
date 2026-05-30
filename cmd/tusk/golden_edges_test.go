package main

import "testing"

// TestGoldenCLI_Edges pins edge add/list/remove against the [edge-types] config:
// the U+2192 add/remove lines, the acyclic-cycle rejection, the legacy
// TYPE/SOURCE/TARGET/SOURCE_PATH table, and edge list's require-a-filter guard.
func TestGoldenCLI_Edges(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:       "edge add creates a typed edge",
			manifest:   edgeManifestBody(),
			setup:      edgeFixture,
			args:       []string{"edge", "add", "--type", "blocks", "--source", "tickets/foo", "--target", "tickets/bar"},
			wantStdout: "Added edge blocks: tickets/foo → tickets/bar\n",
		},
		{
			name:     "edge add rejects a cycle (acyclic)",
			manifest: edgeManifestBody(),
			setup: func(test *testing.T, root string) {
				edgeFixture(test, root)
				addEdge(test, root, "blocks", "tickets/foo", "tickets/bar")
			},
			args:       []string{"edge", "add", "--type", "blocks", "--source", "tickets/bar", "--target", "tickets/foo"},
			wantErr:    true,
			wantStderr: "node: edge would create a cycle: tickets/bar → … → tickets/foo → tickets/bar\n",
		},
		{
			name:       "edge list requires a filter",
			manifest:   edgeManifestBody(),
			args:       []string{"edge", "list"},
			wantErr:    true,
			wantStderr: "specify at least one of --from, --to, --type\n",
		},
		{
			name:     "edge list shows a typed edge",
			manifest: edgeManifestBody(),
			setup: func(test *testing.T, root string) {
				edgeFixture(test, root)
				addEdge(test, root, "blocks", "tickets/foo", "tickets/bar")
			},
			args: []string{"edge", "list", "--type", "blocks"},
			wantStdout: "TYPE    SOURCE       TARGET       SOURCE_PATH\n" +
				"blocks  tickets/foo  tickets/bar  tickets/foo.md\n",
		},
		{
			name:     "edge remove deletes a typed edge",
			manifest: edgeManifestBody(),
			setup: func(test *testing.T, root string) {
				edgeFixture(test, root)
				addEdge(test, root, "blocks", "tickets/foo", "tickets/bar")
			},
			args:       []string{"edge", "remove", "--type", "blocks", "--source", "tickets/foo", "--target", "tickets/bar"},
			wantStdout: "Removed edge blocks: tickets/foo → tickets/bar\n",
		},
	})
}

// edgeFixture writes two ticket nodes and indexes them so edges can reference them.
func edgeFixture(test *testing.T, root string) {
	test.Helper()

	writeFile(test, root, "tickets/foo.md", "---\ntype: ticket\ntitle: Foo\n---\n")
	writeFile(test, root, "tickets/bar.md", "---\ntype: ticket\ntitle: Bar\n---\n")
	reindexWorkspace(test, root)
}

// addEdge adds a CLI-tracked edge and fails the test if it errors.
func addEdge(test *testing.T, root, edgeType, source, target string) {
	test.Helper()

	_, stderr, ok := runCLISplit(root, "edge", "add", "--type", edgeType, "--source", source, "--target", target)

	if !ok {
		test.Fatalf("edge add %s %s->%s: %s", edgeType, source, target, stderr.String())
	}
}
