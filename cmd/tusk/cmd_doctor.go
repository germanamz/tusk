package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Surface validation warnings and index health issues",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			report, runErr := doctor.Run(doctor.Config{
				Nodes:         index.NewNodeRepo(store),
				Edges:         index.NewEdgeRepo(store),
				EmbedQueue:    index.NewEmbedQueueRepo(store),
				WorkflowDrift: index.NewWorkflowDriftRepo(store),
				PropertyDrift: index.NewPropertyDriftRepo(store),
				Embeddings:    index.NewEmbeddingRepo(store),
				Manifest:      loaded,
			})

			if runErr != nil {
				return runErr
			}

			out := cmd.OutOrStdout()

			if len(report.Issues) == 0 {
				_, _ = fmt.Fprintln(out, "doctor: no issues")
			}

			for _, issue := range report.Issues {
				_, _ = fmt.Fprintf(out, "  [%s] %s: %s\n", issue.Kind, issue.NodeID, issue.Message)
			}

			_, _ = fmt.Fprintf(out, "embed queue depth: %d\n", report.EmbedQueueDepth)

			if report.EmbedStats != nil {
				stats := report.EmbedStats

				_, _ = fmt.Fprintf(out, "embed stats: %d nodes, %d chunks (mean %.1f, median %d, max %d)\n",
					stats.TotalNodes, stats.TotalChunks, stats.MeanChunks, stats.MedianChunks, stats.MaxChunks)

				if len(stats.TopByChunks) > 0 {
					_, _ = fmt.Fprintln(out, "top by chunks:")

					for _, entry := range stats.TopByChunks {
						_, _ = fmt.Fprintf(out, "  %s\t%d\n", entry.NodeID, entry.Chunks)
					}
				}
			}

			return nil
		},
	}

	return doctorCmd
}
