package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
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

func TestEdgeRemoveCmd_SweepsLegacyCLIRow(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

	createNode(test, dir, "tickets/a.md", "ticket", "A", "")
	createNode(test, dir, "tickets/b.md", "ticket", "B", "")

	// Seed a legacy __cli__ row directly (bypassing the CLI). This simulates
	// a row left over from a pre-frontmatter "tusk edge add" call.
	store, openErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	edgeRepo := index.NewEdgeRepo(store)

	if upsertErr := edgeRepo.UpsertAll("tickets/a", index.CLISourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.CLISourcePath, Kind: "direct"},
	}); upsertErr != nil {
		test.Fatalf("seed __cli__: %v", upsertErr)
	}

	store.Close()

	// Now run `tusk edge remove`. The sweep should clear the legacy row.
	chdir(test, dir)

	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"edge", "remove", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("edge remove: %v\noutput: %s", execErr, buf.String())
	}

	// Reopen and verify the legacy row is gone.
	store2, openErr2 := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if openErr2 != nil {
		test.Fatalf("reopen: %v", openErr2)
	}

	defer store2.Close()

	rows, listErr := index.NewEdgeRepo(store2).ListBySource("tickets/a")

	if listErr != nil {
		test.Fatalf("list: %v", listErr)
	}

	for _, row := range rows {
		if row.SourcePath == index.CLISourcePath {
			test.Errorf("legacy __cli__ row should have been swept; still present: %+v", row)
		}
	}
}
