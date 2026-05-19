package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdgeAddCmd_PersistsEdgeInIndex(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	createCmd := newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "A", "--path", "tickets/a.md"})

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("create a: %v", execErr)
	}

	create2 := newRootCmd()
	create2.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "B", "--path", "tickets/b.md"})

	if execErr := create2.Execute(); execErr != nil {
		test.Fatalf("create b: %v", execErr)
	}

	output := &bytes.Buffer{}

	addCmd := newRootCmd()
	addCmd.SetOut(output)
	addCmd.SetErr(output)
	addCmd.SetArgs([]string{"edge", "add", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"})

	if execErr := addCmd.Execute(); execErr != nil {
		test.Fatalf("edge add: %v\noutput: %s", execErr, output.String())
	}

	listOutput := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(listOutput)
	listCmd.SetErr(listOutput)
	listCmd.SetArgs([]string{"edge", "list", "--from", "tickets/a"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v\noutput: %s", execErr, listOutput.String())
	}

	listed := listOutput.String()

	if !bytes.Contains([]byte(listed), []byte("blocks")) ||
		!bytes.Contains([]byte(listed), []byte("tickets/a")) ||
		!bytes.Contains([]byte(listed), []byte("tickets/b")) {
		test.Errorf("missing edge triple in list:\n%s", listed)
	}
}

func TestEdgeAddCmd_WritesFrontmatter(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

	createA := newRootCmd()
	createA.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "A", "--path", "tickets/a.md"})

	if execErr := createA.Execute(); execErr != nil {
		test.Fatalf("create a: %v", execErr)
	}

	createB := newRootCmd()
	createB.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "B", "--path", "tickets/b.md"})

	if execErr := createB.Execute(); execErr != nil {
		test.Fatalf("create b: %v", execErr)
	}

	output := &bytes.Buffer{}

	addCmd := newRootCmd()
	addCmd.SetOut(output)
	addCmd.SetErr(output)
	addCmd.SetArgs([]string{"edge", "add", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"})

	if execErr := addCmd.Execute(); execErr != nil {
		test.Fatalf("edge add: %v\noutput: %s", execErr, output.String())
	}

	body, readErr := os.ReadFile(filepath.Join(dir, "tickets/a.md"))

	if readErr != nil {
		test.Fatalf("read tickets/a.md: %v", readErr)
	}

	if !strings.Contains(string(body), "blocks: tickets/b") {
		test.Errorf("expected blocks: tickets/b in source frontmatter, got:\n%s", body)
	}
}

func edgeManifestBody() string {
	return `[workspace]
name = "test"

[edge-types.blocks]
from = ["ticket"]
to = ["ticket"]
cardinality = "many-to-many"
acyclic = true

[edge-types.parent]
from = ["ticket"]
to = ["ticket", "project"]
cardinality = "many-to-one"

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
`
}
