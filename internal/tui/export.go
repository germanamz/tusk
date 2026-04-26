// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/portability"
	"github.com/spf13/cobra"
)

// buildExportCmd builds the `tusk export` subcommand.
func (a *App) buildExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export [--output <path>]",
		Short: "Export the workspace to a JSON dump",
		Long: `Export the entire workspace as a JSON dump suitable for backup,
migration, or rehydration via 'tusk import'.

Output is pretty-printed JSON; pipe through 'jq -c' for compact form.

JSON is the only format. The dump includes every workflow, project,
player, tag, task, relation, annotation, note, and event in the
workspace. The dump's schema_version is 1; future tusk versions may
introduce conversion shims for older versions.`,
		Example: `  # Write to stdout
  tusk export

  # Write to a file (atomic — written via *.tmp then rename)
  tusk export --output /tmp/ws.json`,
		RunE: a.runExport,
	}
	cmd.Flags().StringP("output", "o", "-", `path to write to; "-" for stdout`)
	return cmd
}

func (a *App) runExport(cmd *cobra.Command, _ []string) error {
	if a.portabilitySvc == nil {
		return fmt.Errorf("portability service not configured")
	}
	output, err := cmd.Flags().GetString("output")
	if err != nil {
		return err
	}

	ws, err := a.portabilitySvc.Export(cmd.Context())
	if err != nil {
		return fmt.Errorf("exporting workspace: %w", err)
	}

	if output == "-" {
		return portability.Encode(cmd.OutOrStdout(), ws)
	}

	dir := filepath.Dir(output)
	tmp, err := os.CreateTemp(dir, filepath.Base(output)+".tmp.*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if err := portability.Encode(tmp, ws); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("encoding workspace: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, output); err != nil {
		cleanup()
		return fmt.Errorf("renaming temp file to %s: %w", output, err)
	}
	return nil
}
