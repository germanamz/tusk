package main

import (
	"bytes"
	"fmt"
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

	if !bytes.Contains(listOutput.Bytes(), []byte("blocks")) || !bytes.Contains(listOutput.Bytes(), []byte("tickets/b")) {
		test.Errorf("missing edge in list:\n%s", listOutput.String())
	}
}

func TestEdgeAddCmd_HonorsExplicitOrdinal(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	for _, name := range []string{"a", "b", "c"} {
		createCmd := newRootCmd()
		createCmd.SetArgs([]string{"node", "create", "--type", "ticket", "--title", name, "--path", "tickets/" + name + ".md"})

		if execErr := createCmd.Execute(); execErr != nil {
			test.Fatalf("create %s: %v", name, execErr)
		}
	}

	// Two children pointing at the same target should each get a distinct ordinal
	// when the caller passes --ordinal explicitly.
	for index, source := range []string{"tickets/a", "tickets/b"} {
		buffer := &bytes.Buffer{}
		addCmd := newRootCmd()
		addCmd.SetOut(buffer)
		addCmd.SetErr(buffer)
		addCmd.SetArgs([]string{
			"edge", "add",
			"--type", "parent",
			"--source", source,
			"--target", "tickets/c",
			"--ordinal", fmt.Sprintf("%d", index),
		})

		if execErr := addCmd.Execute(); execErr != nil {
			test.Fatalf("edge add %s: %v\noutput: %s", source, execErr, buffer.String())
		}
	}

	listBuffer := &bytes.Buffer{}
	listCmd := newRootCmd()
	listCmd.SetOut(listBuffer)
	listCmd.SetErr(listBuffer)
	listCmd.SetArgs([]string{"edge", "list", "--type", "parent"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v\noutput: %s", execErr, listBuffer.String())
	}

	output := listBuffer.String()

	if !strings.Contains(output, "tickets/a  tickets/c  0") {
		test.Errorf("expected tickets/a to have ordinal 0, got:\n%s", output)
	}

	if !strings.Contains(output, "tickets/b  tickets/c  1") {
		test.Errorf("expected tickets/b to have ordinal 1, got:\n%s", output)
	}
}

func TestEdgeAddCmd_RejectsNegativeOrdinalBeyondSentinel(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	createCmd := newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "A", "--path", "tickets/a.md"})

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("create a: %v", execErr)
	}

	createCmd = newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "B", "--path", "tickets/b.md"})

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("create b: %v", execErr)
	}

	buffer := &bytes.Buffer{}
	addCmd := newRootCmd()
	addCmd.SetOut(buffer)
	addCmd.SetErr(buffer)
	addCmd.SilenceErrors = true
	addCmd.SilenceUsage = true
	addCmd.SetArgs([]string{
		"edge", "add",
		"--type", "blocks",
		"--source", "tickets/a",
		"--target", "tickets/b",
		"--ordinal", "-2",
	})

	if execErr := addCmd.Execute(); execErr == nil {
		test.Fatalf("expected error for --ordinal -2, got nil; output: %s", buffer.String())
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
