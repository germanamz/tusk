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

func newEdgeAddCmd() *cobra.Command {
	var (
		edgeType   string
		source     string
		target     string
		ordinalArg int
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a typed edge from one node to another",
		Long: `Add a typed edge from one node to another.

The edge kind must be declared in tusk.toml. CLI-added edges are
attributed to a synthetic "__cli__" source path so the next reindex of
either involved file does not clobber them.

When --ordinal is unset (-1), the next free ordinal is auto-assigned
across the same (source, type) group of CLI-added edges. Pass --ordinal
to control placement explicitly — useful for ordered edges where the
intended ordering key is the target (e.g. WBS child ordering under a
shared parent).`,
		Example: `  # Mark T-001 as blocking T-002
  tusk edge add --type blocks --source tickets/T-001 --target tickets/T-002

  # Add multiple edges as part of a script
  tusk edge add --type mentions --source tickets/T-003 --target notes/2026-05-16
  tusk edge add --type owned-by --source tickets/T-003 --target people/alice

  # Order children explicitly under a shared parent
  tusk edge add --type wbs-parent --source wbs/proj/s1 --target wbs/proj --ordinal 0
  tusk edge add --type wbs-parent --source wbs/proj/s2 --target wbs/proj --ordinal 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if edgeType == "" || source == "" || target == "" {
				return fmt.Errorf("--type, --source, and --target are required")
			}

			if ordinalArg < -1 {
				return fmt.Errorf("--ordinal must be >= 0 (use -1 or omit for auto-assign)")
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

			edgeDef, declared := loaded.EdgeTypes[edgeType]

			if !declared {
				return fmt.Errorf("edge type %q not declared in manifest", edgeType)
			}

			return withWorkspaceLock(ws, func() error {
				store, openErr := index.Open(ws.IndexPath)

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

				edgeRepo := index.NewEdgeRepo(store)

				if edgeDef.Acyclic {
					existing, listErr := edgeRepo.ListByType(edgeType)

					if listErr != nil {
						return listErr
					}

					adjacency := buildAdjacency(existing)

					if cycleErr := node.DetectCycle(node.CycleProbe{EdgeType: edgeType, Source: source, Target: target}, adjacency); cycleErr != nil {
						return cycleErr
					}
				}

				existingForSource, listErr := edgeRepo.ListBySource(source)

				if listErr != nil {
					return listErr
				}

				cliExisting := filterCLI(existingForSource)

				ordinal := ordinalArg

				if ordinal < 0 {
					ordinal = nextOrdinalFor(cliExisting, edgeType)
				}

				cliExisting = append(cliExisting, index.EdgeRow{
					Type:       edgeType,
					SourceID:   source,
					TargetID:   target,
					Ordinal:    ordinal,
					SourcePath: cliSourcePath,
				})

				if upsertErr := edgeRepo.UpsertAll(source, cliSourcePath, cliExisting); upsertErr != nil {
					return upsertErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Added edge %s: %s → %s\n", edgeType, source, target)

				return nil
			})
		},
	}

	addCmd.Flags().StringVar(&edgeType, "type", "", "edge type (must be declared in tusk.toml)")
	addCmd.Flags().StringVar(&source, "source", "", "source node id (workspace-relative path without extension)")
	addCmd.Flags().StringVar(&target, "target", "", "target node id")
	addCmd.Flags().IntVar(&ordinalArg, "ordinal", -1, "edge ordinal (>= 0); -1 (default) auto-assigns the next free value for this (source, type) group")

	return addCmd
}

func buildAdjacency(rows []index.EdgeRow) map[string][]string {
	adjacency := map[string][]string{}

	for _, row := range rows {
		adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
	}

	return adjacency
}

func filterCLI(rows []index.EdgeRow) []index.EdgeRow {
	var filtered []index.EdgeRow

	for _, row := range rows {
		if row.SourcePath == cliSourcePath {
			filtered = append(filtered, row)
		}
	}

	return filtered
}

func nextOrdinalFor(rows []index.EdgeRow, edgeType string) int {
	maxOrdinal := -1

	for _, row := range rows {
		if row.Type != edgeType {
			continue
		}

		if row.Ordinal > maxOrdinal {
			maxOrdinal = row.Ordinal
		}
	}

	return maxOrdinal + 1
}
