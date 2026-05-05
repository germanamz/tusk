package main

import (
	"context"
	"time"

	"github.com/germanamz/tusk/internal/lock"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeCmd() *cobra.Command {
	nodeCmd := &cobra.Command{
		Use:   "node",
		Short: "Manage individual nodes (create, get, list)",
	}

	nodeCmd.AddCommand(newNodeCreateCmd())
	nodeCmd.AddCommand(newNodeGetCmd())
	nodeCmd.AddCommand(newNodeListCmd())
	nodeCmd.AddCommand(newNodeDeleteCmd())

	return nodeCmd
}

// withWorkspaceLock acquires the workspace lock, runs body, and always releases.
// Returns the lock-acquisition error or body's error.
func withWorkspaceLock(ws *workspace.Workspace, body func() error) error {
	lockHandle, lockNewErr := lock.NewWorkspaceLock(ws.Root)

	if lockNewErr != nil {
		return lockNewErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if acquireErr := lockHandle.Acquire(ctx); acquireErr != nil {
		return acquireErr
	}

	defer func() { _ = lockHandle.Release() }()

	return body()
}
