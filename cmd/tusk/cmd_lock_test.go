package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/lock"
)

func TestNodeCreateCmd_BlocksOnWorkspaceLock(test *testing.T) {
	tmpDir := initWorkspace(test)

	holder, _ := lock.NewWorkspaceLock(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if acquireErr := holder.Acquire(ctx); acquireErr != nil {
		test.Fatalf("holder Acquire: %v", acquireErr)
	}

	test.Cleanup(func() { holder.Release() })

	createCmd := newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "note", "--path", "blocked.md"})

	createErr := createCmd.Execute()

	if createErr == nil {
		test.Fatalf("expected error when lock is held")
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, "blocked.md")); statErr == nil {
		test.Errorf("file should NOT have been written while lock was held")
	}
}
