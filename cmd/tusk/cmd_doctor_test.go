package main

import (
	"strings"
	"testing"
)

func TestDoctor_PrintsCleanReport(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "no issues") {
		test.Errorf("expected 'no issues', got:\n%s", out)
	}
}
