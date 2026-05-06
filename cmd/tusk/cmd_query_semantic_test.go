package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestQueryCmd_SemanticErrorsWhenEmbeddingsAbsent(test *testing.T) {
	initWorkspace(test)

	createCmd := newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "X", "--path", "x.md"})

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "auth bug"})

	queryErr := queryCmd.Execute()

	if queryErr == nil {
		test.Fatalf("expected error when embeddings provider not configured")
	}

	if !strings.Contains(queryErr.Error(), "embeddings") {
		test.Errorf("error should mention embeddings: %v", queryErr)
	}
}
