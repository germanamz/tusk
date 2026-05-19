package main

import (
	"bytes"
	"testing"
)

func TestEdgeRemoveCmd_DropsEdgeFromIndex(test *testing.T) {
	test.Skip("superseded by TestEdgeRemoveCmd_RemovesFromFrontmatter once edge remove writes frontmatter (Task 3.3)")

	initWorkspaceWithManifest(test, edgeManifestBody())

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "A", "--path", "tickets/a.md"},
		{"node", "create", "--type", "ticket", "--title", "B", "--path", "tickets/b.md"},
		{"edge", "add", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	removeCmd := newRootCmd()
	removeCmd.SetArgs([]string{"edge", "remove", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"})

	if execErr := removeCmd.Execute(); execErr != nil {
		test.Fatalf("edge remove: %v", execErr)
	}

	listOutput := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(listOutput)
	listCmd.SetErr(listOutput)
	listCmd.SetArgs([]string{"edge", "list", "--from", "tickets/a"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v", execErr)
	}

	if bytes.Contains(listOutput.Bytes(), []byte("tickets/b")) {
		test.Errorf("edge should have been removed, list still shows it:\n%s", listOutput.String())
	}
}
