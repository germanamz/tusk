package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestEdgeListCmd_FiltersByFromTypeAndTo(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "A", "--path", "tickets/a.md"},
		{"node", "create", "--type", "ticket", "--title", "B", "--path", "tickets/b.md"},
		{"node", "create", "--type", "ticket", "--title", "C", "--path", "tickets/c.md"},
		{"edge", "add", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"},
		{"edge", "add", "--type", "parent", "--source", "tickets/a", "--target", "tickets/c"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--from", "tickets/a"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list --from: %v", execErr)
		}

		body := out.String()

		if strings.Count(body, "tickets/a") < 2 {
			test.Errorf("expected at least 2 rows with source tickets/a:\n%s", body)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--type", "blocks"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list --type: %v", execErr)
		}

		body := out.String()

		if !strings.Contains(body, "blocks") || strings.Contains(body, "parent") {
			test.Errorf("expected blocks-only:\n%s", body)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--to", "tickets/c"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list --to: %v", execErr)
		}

		body := out.String()

		if !strings.Contains(body, "tickets/c") || strings.Contains(body, "tickets/b") {
			test.Errorf("expected target=tickets/c only:\n%s", body)
		}
	}
}
