package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdgeRemoveCmd_DropsEdgeFromIndex(test *testing.T) {
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

func TestEdgeRemoveCmd_RemovesFromFrontmatter(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

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

	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"edge", "remove", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("edge remove: %v\noutput: %s", execErr, buf.String())
	}

	body, readErr := os.ReadFile(filepath.Join(dir, "tickets/a.md"))

	if readErr != nil {
		test.Fatalf("read tickets/a.md: %v", readErr)
	}

	if strings.Contains(string(body), "blocks") {
		test.Errorf("blocks key should have been removed, got:\n%s", body)
	}
}
