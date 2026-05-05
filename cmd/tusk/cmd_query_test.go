package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestQueryCmd_FiltersByType(test *testing.T) {
	initWorkspace(test)

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "T1", "--path", "tickets/t1.md"},
		{"node", "create", "--type", "ticket", "--title", "T2", "--path", "tickets/t2.md"},
		{"node", "create", "--type", "note", "--title", "N1", "--path", "notes/n1.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=ticket"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query: %v", execErr)
	}

	body := out.String()

	if strings.Contains(body, "notes/n1") {
		test.Errorf("note should be excluded: %s", body)
	}

	if !strings.Contains(body, "tickets/t1") || !strings.Contains(body, "tickets/t2") {
		test.Errorf("missing tickets: %s", body)
	}
}

func TestQueryCmd_TakeAndSkip(test *testing.T) {
	initWorkspace(test)

	for index := 0; index < 5; index++ {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "n", "--path", "notes/n" + string(rune('0'+index)) + ".md"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create: %v", execErr)
		}
	}

	out := &bytes.Buffer{}

	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"query", "type=note", "--take", "2", "--skip", "1"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("query: %v", execErr)
	}

	body := out.String()

	dataLines := strings.Count(strings.TrimSpace(body), "\n")

	if dataLines != 2 {
		test.Errorf("expected 2 data rows (got %d):\n%s", dataLines, body)
	}
}

func TestQueryCmd_ErrorsWithoutFilter(test *testing.T) {
	initWorkspace(test)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"query"})

	if execErr := cmd.Execute(); execErr == nil {
		test.Fatalf("expected error when filter argument is missing")
	}
}
