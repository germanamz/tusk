package main

import (
	"strings"
	"testing"
)

func TestMCP_ParsesTransportFlag(test *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"mcp", "--help"})

	out := captureStdout(test, func() {
		_ = cmd.Execute()
	})

	if !strings.Contains(out, "--transport") {
		test.Errorf("expected --transport flag in help, got:\n%s", out)
	}

	if !strings.Contains(out, "stdio") || !strings.Contains(out, "sse") {
		test.Errorf("expected stdio and sse mentioned, got:\n%s", out)
	}
}

func TestMCP_RejectsUnknownTransport(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("mcp", "--transport", "bogus")

	if runErr == nil {
		test.Fatalf("expected error, got out:\n%s", out)
	}
}
