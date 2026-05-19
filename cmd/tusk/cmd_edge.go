package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newEdgeCmd() *cobra.Command {
	edgeCmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edges between nodes (add, remove, list)",
		Long: `Manage edges between nodes.

Edges have a typed kind (declared in tusk.toml's [edge_types] table), a
source node, and a target node. They are declared inline in a node's
frontmatter (the file owns them); "tusk edge add" mutates that frontmatter
and refreshes the index for the affected source file.

Use "tusk edge list" with --from/--to/--type to filter.`,
	}

	edgeCmd.AddCommand(newEdgeAddCmd())
	edgeCmd.AddCommand(newEdgeRemoveCmd())
	edgeCmd.AddCommand(newEdgeListCmd())

	return edgeCmd
}

// cliSourcePath is the legacy synthetic source_path attributed to edges added
// via earlier versions of `tusk edge add`. Retained for the migration path
// that sweeps legacy rows; new writes always use the real source file path.
const cliSourcePath = "__cli__"

// reindexSingle re-parses sourceID's markdown file and upserts the resulting
// edge rows under the file's real source path. Used by edge mutations that
// only touch a single source file so the index reflects the new state before
// the command returns (without scanning the entire workspace).
func reindexSingle(ws *workspace.Workspace, store *index.Index, loaded *manifest.Manifest, sourceID string) error {
	relPath := sourceID + ".md"
	absPath := filepath.Join(ws.Root, relPath)

	content, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return fmt.Errorf("reindex %s: %w", relPath, readErr)
	}

	parsed, parseErr := node.ParseFile(relPath, content)

	if parseErr != nil {
		return fmt.Errorf("reindex %s: %w", relPath, parseErr)
	}

	if resolveErr := node.ResolveEdges(parsed, loaded.EdgeTypes); resolveErr != nil {
		return fmt.Errorf("reindex %s: %w", relPath, resolveErr)
	}

	var edgeRows []index.EdgeRow

	for edgeType, targets := range parsed.Edges {
		for _, targetID := range targets {
			edgeRows = append(edgeRows, index.EdgeRow{
				Type:       edgeType,
				SourceID:   parsed.ID,
				TargetID:   targetID,
				SourcePath: parsed.Path,
			})
		}
	}

	edgeRepo := index.NewEdgeRepo(store)

	if upsertErr := edgeRepo.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
		return fmt.Errorf("reindex %s: %w", relPath, upsertErr)
	}

	return nil
}
