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
func (app *App) buildExportCmd() *cobra.Command {
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
		RunE: app.runExport,
	}
	cmd.Flags().StringP("output", "o", "-", `path to write to; "-" for stdout`)
	return cmd
}

func (app *App) runExport(cmd *cobra.Command, _ []string) error {
	if app.portabilitySvc == nil {
		return fmt.Errorf("portability service not configured")
	}
	output, outputErr := cmd.Flags().GetString("output")

	if outputErr != nil {
		return outputErr
	}

	ws, exportErr := app.portabilitySvc.Export(cmd.Context())

	if exportErr != nil {
		return fmt.Errorf("exporting workspace: %w", exportErr)
	}

	if output == "-" {
		return portability.Encode(cmd.OutOrStdout(), ws)
	}

	dir := filepath.Dir(output)
	tmp, tmpErr := os.CreateTemp(dir, filepath.Base(output)+".tmp.*")

	if tmpErr != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, tmpErr)
	}

	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if encodeErr := portability.Encode(tmp, ws); encodeErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("encoding workspace: %w", encodeErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return fmt.Errorf("closing temp file: %w", closeErr)
	}
	if renameErr := os.Rename(tmpName, output); renameErr != nil {
		cleanup()
		return fmt.Errorf("renaming temp file to %s: %w", output, renameErr)
	}
	return nil
}
