package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newEdgeRemoveCmd() *cobra.Command {
	var (
		edgeType string
		source   string
		target   string
	)

	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a specific edge by source, kind, and target",
		Long: `Remove a specific edge identified by its source, kind, and target.

The edge is removed from the source node's markdown frontmatter and the
index is updated to match. Any legacy "__cli__" or "__mcp__" rows for the
same (type, source, target) triple are also cleared from the index as a
back-compatibility sweep — those rows are remnants of pre-frontmatter
"tusk edge add" / "tusk_edge_add" MCP calls. A future version of "tusk doctor"
will auto-migrate them into source frontmatter.`,
		Example: `  # Remove a blocks edge added via "edge add"
  tusk edge remove --type blocks --source tickets/T-001 --target tickets/T-002`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if edgeType == "" || source == "" || target == "" {
				return fmt.Errorf("--type, --source, and --target are required")
			}

			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			if _, declared := loaded.EdgeTypes[edgeType]; !declared {
				return fmt.Errorf("edge type %q not declared in manifest", edgeType)
			}

			return withWorkspaceLock(ws, func() error {
				store, openErr := index.Open(ws.IndexPath)

				if openErr != nil {
					return openErr
				}

				defer store.Close()

				if writeErr := node.RemoveEdgeFromFrontmatter(ws.Root, source, edgeType, target, loaded.EdgeTypes); writeErr != nil {
					return writeErr
				}

				edgeRepo := index.NewEdgeRepo(store)

				if reindexErr := node.ReindexSource(ws.Root, edgeRepo, loaded.EdgeTypes, source); reindexErr != nil {
					return reindexErr
				}

				// Back-compat: also clear any legacy __cli__/__mcp__ row for this triple.
				legacy, listErr := edgeRepo.ListBySource(source)

				if listErr != nil {
					return fmt.Errorf("edge remove: list legacy rows: %w", listErr)
				}

				var keptLegacyCLI, keptLegacyMCP []index.EdgeRow

				for _, row := range legacy {
					matchesTriple := row.Type == edgeType && row.TargetID == target

					switch row.SourcePath {
					case index.CLISourcePath:
						if !matchesTriple {
							keptLegacyCLI = append(keptLegacyCLI, row)
						}
					case index.MCPSourcePath:
						if !matchesTriple {
							keptLegacyMCP = append(keptLegacyMCP, row)
						}
					}
				}

				if upsertErr := edgeRepo.UpsertAll(source, index.CLISourcePath, keptLegacyCLI); upsertErr != nil {
					return upsertErr
				}

				if upsertErr := edgeRepo.UpsertAll(source, index.MCPSourcePath, keptLegacyMCP); upsertErr != nil {
					return upsertErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed edge %s: %s → %s\n", edgeType, source, target)

				return nil
			})
		},
	}

	removeCmd.Flags().StringVar(&edgeType, "type", "", "edge type")
	removeCmd.Flags().StringVar(&source, "source", "", "source node id")
	removeCmd.Flags().StringVar(&target, "target", "", "target node id")

	return removeCmd
}
