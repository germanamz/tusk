package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
	"github.com/spf13/cobra"
)

func newEdgeAddCmd() *cobra.Command {
	var (
		edgeType string
		source   string
		target   string
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a typed edge from one node to another",
		Long: `Add a typed edge from one node to another by writing the edge into the
source node's frontmatter.

The edge kind must be declared in tusk.toml's [edge-types.<name>]. The
source's node type must be in the edge's "from" list, and the target's
node type must be in the edge's "to" list.

What this command actually does:

  1. Reads the source file's current frontmatter.
  2. Adds the target under the edge-name key, respecting cardinality:
       * one-to-one / many-to-one: scalar string; rejects on conflict.
       * one-to-many / many-to-many: list; appends if absent (dedup).
  3. Atomically rewrites the file with the new frontmatter.
  4. Reindexes the source file so the new edge is queryable immediately.

Idempotent: adding an edge that already exists is a no-op. To replace a
single-target edge, run "tusk edge remove" first.

The change is durable: the edge lives in git-tracked markdown, not in the
index database. Running "rm .tusk/index.db && tusk reindex" recovers the
same graph state.`,
		Example: `  # Mark T-001 as blocking T-002
  tusk edge add --type blocks --source tickets/T-001 --target tickets/T-002

  # Add multiple edges as part of a script
  tusk edge add --type mentions --source tickets/T-003 --target notes/2026-05-16
  tusk edge add --type owned-by --source tickets/T-003 --target people/alice`,
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

			manifest.MergeBuiltinPacks(loaded)
			edgeDef, declared := loaded.EdgeTypes[edgeType]

			if !declared {
				return fmt.Errorf("edge type %q not declared in manifest", edgeType)
			}

			return withWorkspaceLock(ws, func() error {
				store, openErr := indexopen.OpenOrRebuild(cmd.Context(), indexopen.Config{
					IndexPath: ws.IndexPath,
					ReindexFactory: func(idx *index.Index) reindex.Config {
						return reindex.Config{
							Root:      ws.Root,
							Repo:      index.NewNodeRepo(idx),
							Edges:     index.NewEdgeRepo(idx),
							EdgeTypes: loaded.EdgeTypes,
						}
					},
					Logger: func(msg string) {
						_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msg)
					},
				})

				if openErr != nil {
					return openErr
				}

				defer store.Close()

				nodeRepo := index.NewNodeRepo(store)

				sourceRow, sourceErr := nodeRepo.Get(source)

				if sourceErr != nil {
					return fmt.Errorf("source: %w", sourceErr)
				}

				if !edgeDef.AllowsSource(sourceRow.Type) {
					return fmt.Errorf("edge type %q does not allow source type %q", edgeType, sourceRow.Type)
				}

				if targetRow, getErr := nodeRepo.Get(target); getErr == nil {
					if !edgeDef.AllowsTarget(targetRow.Type) {
						return fmt.Errorf("edge type %q does not allow target type %q", edgeType, targetRow.Type)
					}
				}

				if edgeDef.Acyclic {
					edgeRepo := index.NewEdgeRepo(store)
					existing, listErr := edgeRepo.ListByType(edgeType)

					if listErr != nil {
						return listErr
					}

					adjacency := buildAdjacency(existing)

					if cycleErr := node.DetectCycle(node.CycleProbe{EdgeType: edgeType, Source: source, Target: target}, adjacency); cycleErr != nil {
						return cycleErr
					}
				}

				if writeErr := node.AddEdgeToFrontmatter(ws.Root, source, edgeType, target, loaded.EdgeTypes); writeErr != nil {
					return writeErr
				}

				if reindexErr := node.ReindexSource(ws.Root, index.NewEdgeRepo(store), loaded.EdgeTypes, loaded.NodeTypes, source); reindexErr != nil {
					return reindexErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Added edge %s: %s → %s\n", edgeType, source, target)

				return nil
			})
		},
	}

	addCmd.Flags().StringVar(&edgeType, "type", "", "edge type (must be declared in tusk.toml)")
	addCmd.Flags().StringVar(&source, "source", "", "source node id (workspace-relative path without extension)")
	addCmd.Flags().StringVar(&target, "target", "", "target node id")

	return addCmd
}

func buildAdjacency(rows []index.EdgeRow) map[string][]string {
	adjacency := map[string][]string{}

	for _, row := range rows {
		adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
	}

	return adjacency
}
