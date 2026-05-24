package main

import (
	"bytes"
	"encoding/json"
	"strings"
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

// TestNodeGetCmd_CompactRespectsInclude asserts that the compact renderer
// drops edges and properties when only --include body was requested. A
// previous version always handed Body / Properties / Edges to the renderer
// regardless of the include filter, causing the compact path to leak data
// the JSON path correctly filtered out.
func TestNodeGetCmd_CompactRespectsInclude(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	// Source ticket carries an outgoing edge plus a custom property.
	source := newRootCmd()
	source.SetArgs([]string{
		"node", "create",
		"--type", "ticket",
		"--title", "Source",
		"--path", "tickets/src.md",
		"--prop", "priority=1",
	})

	if execErr := source.Execute(); execErr != nil {
		test.Fatalf("create source: %v", execErr)
	}

	target := newRootCmd()
	target.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "Target", "--path", "tickets/tgt.md"})

	if execErr := target.Execute(); execErr != nil {
		test.Fatalf("create target: %v", execErr)
	}

	addEdge := newRootCmd()
	addEdge.SetArgs([]string{"edge", "add", "--type", "blocks", "--source", "tickets/src", "--target", "tickets/tgt"})

	if execErr := addEdge.Execute(); execErr != nil {
		test.Fatalf("edge add: %v", execErr)
	}

	output := &bytes.Buffer{}

	getCmd := newRootCmd()
	getCmd.SetOut(output)
	getCmd.SetErr(output)
	getCmd.SetArgs([]string{"node", "get", "tickets/src", "--include", "body", "--format", "compact"})

	if execErr := getCmd.Execute(); execErr != nil {
		test.Fatalf("get: %v\noutput: %s", execErr, output.String())
	}

	body := output.String()

	if !strings.Contains(body, "tickets/src") {
		test.Errorf("expected id in compact output:\n%s", body)
	}

	if strings.Contains(body, "→") || strings.Contains(body, "blocks") {
		test.Errorf("expected no edges rendered when only --include body was requested:\n%s", body)
	}

	if strings.Contains(body, "priority=1") {
		test.Errorf("expected no properties rendered when only --include body was requested:\n%s", body)
	}
}
