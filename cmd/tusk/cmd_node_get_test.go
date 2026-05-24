package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNodeGetCmd_PrintsFrontmatterAndBody(test *testing.T) {
	initWorkspace(test)

	create := newRootCmd()
	create.SetArgs([]string{"node", "create", "--type", "note", "--title", "Hi", "--path", "x.md"})

	if execErr := create.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	output := &bytes.Buffer{}

	getCmd := newRootCmd()
	getCmd.SetOut(output)
	getCmd.SetErr(output)
	getCmd.SetArgs([]string{"node", "get", "x"})

	if execErr := getCmd.Execute(); execErr != nil {
		test.Fatalf("get: %v\noutput: %s", execErr, output.String())
	}

	if !bytes.Contains(output.Bytes(), []byte("type: note")) {
		test.Errorf("output missing type: %s", output.String())
	}

	if !bytes.Contains(output.Bytes(), []byte("title: Hi")) {
		test.Errorf("output missing title: %s", output.String())
	}
}

// TestNodeGetCmd_StructuredJSON exercises the new --include/--format JSON
// branch. The legacy raw-file output is preserved by default; with --json
// the command emits a structured envelope honoring the include filter.
func TestNodeGetCmd_StructuredJSON(test *testing.T) {
	initWorkspace(test)

	create := newRootCmd()
	create.SetArgs([]string{"node", "create", "--type", "note", "--title", "Hi", "--path", "x.md"})

	if execErr := create.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	output := &bytes.Buffer{}

	getCmd := newRootCmd()
	getCmd.SetOut(output)
	getCmd.SetErr(output)
	getCmd.SetArgs([]string{"node", "get", "x", "--include", "body", "--json"})

	if execErr := getCmd.Execute(); execErr != nil {
		test.Fatalf("get: %v\noutput: %s", execErr, output.String())
	}

	var payload map[string]any

	if unmarshalErr := json.Unmarshal(output.Bytes(), &payload); unmarshalErr != nil {
		test.Fatalf("expected JSON output, got: %s\nerr: %v", output.String(), unmarshalErr)
	}

	if payload["id"] != "x" {
		test.Errorf("id = %v, want x", payload["id"])
	}

	if _, hasBody := payload["body"]; !hasBody {
		test.Errorf("expected body key in payload: %+v", payload)
	}

	if _, hasEdges := payload["edges"]; hasEdges {
		test.Errorf("expected edges to be omitted (not requested): %v", payload["edges"])
	}

	if _, hasProps := payload["properties"]; hasProps {
		test.Errorf("expected properties to be omitted (not requested): %v", payload["properties"])
	}
}
