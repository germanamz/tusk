package main

import (
	"os"
	"testing"
)

// TestMain keeps the in-process CLI tests hermetic: a TUSK_WORKSPACE exported
// in the developer's shell would point every command at that vault instead of
// the test's temp workspace.
func TestMain(suite *testing.M) {
	_ = os.Unsetenv(workspaceEnv)

	os.Exit(suite.Run())
}
