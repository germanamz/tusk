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
		Short: "Manage individual nodes (create, get, list, modify, move, delete)",
		Long: `Manage individual nodes in the workspace.

A node is one markdown file with YAML frontmatter declaring its type and
properties. Example:

  ---
  type: ticket
  title: Fix login bug
  priority: high
  blocks:
    - tickets/T-002
  ---

  # Fix login bug

  ...body...

Top-level frontmatter keys split into two namespaces, enforced by the
manifest at load time:

  * Property keys (e.g. priority, title) — declared in
    [node-types.<type>].properties in tusk.toml.
  * Edge keys (e.g. blocks) — names that match an [edge-types.<name>]
    declaration in tusk.toml. The value is a target node id (scalar) or
    list of target ids; reindex turns each into an indexed edge.

A few keys are reserved: "type" (required, picks the node-type schema),
"title", and any "status-property" declared by an active behavior pack.

Body wikilinks ([[path/to/target]]) materialize as implicit "references"
edges when the manifest declares that edge type.

The node subcommands are thin wrappers over the same internal service the
watcher and reindex use, so creating a node by CLI and saving a file in
your editor produce identical index state.

Use "tusk node create" to author a new file, "tusk node modify" to change
frontmatter properties, "tusk node move" to atomically rename a node and
rewrite all referring edges, and "tusk node list" to query the index with
the filter grammar.`,
	}

	nodeCmd.AddCommand(newNodeCreateCmd())
	nodeCmd.AddCommand(newNodeGetCmd())
	nodeCmd.AddCommand(newNodeListCmd())
	nodeCmd.AddCommand(newNodeDeleteCmd())
	nodeCmd.AddCommand(newNodeMoveCmd())
	nodeCmd.AddCommand(newNodeModifyCmd())

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
