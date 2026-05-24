package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNodeListCmd_PrintsCreatedNodes(test *testing.T) {
	initWorkspace(test)

	first := newRootCmd()
	first.SetArgs([]string{"node", "create", "--type", "note", "--path", "a.md"})

	if execErr := first.Execute(); execErr != nil {
		test.Fatalf("first: %v", execErr)
	}

	second := newRootCmd()
	second.SetArgs([]string{"node", "create", "--type", "ticket", "--path", "b.md"})

	if execErr := second.Execute(); execErr != nil {
		test.Fatalf("second: %v", execErr)
	}

	output := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(output)
	listCmd.SetErr(output)
	listCmd.SetArgs([]string{"node", "list"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	if !bytes.Contains(output.Bytes(), []byte("a")) || !bytes.Contains(output.Bytes(), []byte("b")) {
		test.Errorf("missing rows: %s", output.String())
	}
}

func TestNodeListCmd_PositionalFilterByType(test *testing.T) {
	initWorkspace(test)

	for _, args := range [][]string{
		{"node", "create", "--type", "note", "--path", "a.md"},
		{"node", "create", "--type", "ticket", "--path", "b.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create: %v", execErr)
		}
	}

	output := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(output)
	listCmd.SetErr(output)
	listCmd.SetArgs([]string{"node", "list", "type=ticket"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	body := output.String()

	if strings.Contains(body, "\na\t") {
		test.Errorf("expected only ticket: %s", body)
	}

	if !strings.Contains(body, "b") {
		test.Errorf("missing b: %s", body)
	}
}

func TestNodeListCmd_IncludeBodyJSON(test *testing.T) {
	initWorkspace(test)

	create := newRootCmd()
	create.SetArgs([]string{"node", "create", "--type", "note", "--path", "a.md", "--title", "Alpha"})

	if execErr := create.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	out := &bytes.Buffer{}

	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"node", "list", "--include", "body", "--json"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	body := out.String()

	if !strings.Contains(body, `"body"`) {
		test.Errorf("expected body key in JSON output:\n%s", body)
	}

	if !strings.Contains(body, "Alpha") {
		test.Errorf("expected Alpha in body:\n%s", body)
	}
}

func TestNodeListCmd_FormatCompact(test *testing.T) {
	initWorkspace(test)

	create := newRootCmd()
	create.SetArgs([]string{"node", "create", "--type", "note", "--path", "a.md", "--title", "Alpha"})

	if execErr := create.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	out := &bytes.Buffer{}

	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"node", "list", "--format", "compact"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	body := out.String()

	if !strings.Contains(body, "a") || !strings.Contains(body, "Alpha") {
		test.Errorf("expected compact form to include a and Alpha:\n%s", body)
	}

	// Compact form must not include the legacy header row.
	if strings.Contains(body, "TYPE") {
		test.Errorf("compact form should not include uppercase header row:\n%s", body)
	}
}
