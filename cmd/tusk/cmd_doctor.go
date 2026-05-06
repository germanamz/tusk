package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
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

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			report, runErr := doctor.Run(doctor.Config{
				Nodes:      index.NewNodeRepo(store),
				Edges:      index.NewEdgeRepo(store),
				EmbedQueue: index.NewEmbedQueueRepo(store),
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

			return nil
		},
	}

	return doctorCmd
}
