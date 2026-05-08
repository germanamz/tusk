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
