package main

import (
	"testing"
)

func TestWatchCmd_HelpRenders(test *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"watch", "--help"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Errorf("watch --help: %v", execErr)
	}
}
