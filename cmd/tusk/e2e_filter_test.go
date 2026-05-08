package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestE2E_FilterPipeline(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "Foo", "--path", "tickets/foo.md"},
		{"node", "create", "--type", "ticket", "--title", "Bar", "--path", "tickets/bar.md"},
		{"node", "create", "--type", "note", "--title", "N1", "--path", "notes/n1.md"},
		{"edge", "add", "--type", "blocks", "--source", "tickets/foo", "--target", "tickets/bar"},
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
		cmd.SetArgs([]string{"query", "type=ticket"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("simple query: %v", execErr)
		}

		if !strings.Contains(out.String(), "tickets/foo") || !strings.Contains(out.String(), "tickets/bar") {
			test.Errorf("missing tickets: %s", out.String())
		}

		if strings.Contains(out.String(), "notes/n1") {
			test.Errorf("notes should be excluded: %s", out.String())
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"query", "blocks->"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("edge probe: %v", execErr)
		}

		// Only tickets/foo has an outgoing blocks edge.
		if !strings.Contains(out.String(), "tickets/foo") {
			test.Errorf("missing tickets/foo: %s", out.String())
		}

		if strings.Contains(out.String(), "tickets/bar") {
			test.Errorf("bar should be excluded: %s", out.String())
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"query", "type=ticket OR type=note", "--sort", "+title"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("compound query with sort: %v", execErr)
		}

		body := out.String()
		barPos := strings.Index(body, "Bar")
		fooPos := strings.Index(body, "Foo")

		if barPos < 0 || fooPos < 0 {
			test.Fatalf("missing rows: %s", body)
		}

		if barPos > fooPos {
			test.Errorf("expected ascending sort: Bar before Foo. body:\n%s", body)
		}
	}
}
