package main

import (
	"bytes"
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
