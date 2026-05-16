package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

const defaultManifestTemplate = `[workspace]
name = %q
ignore = []
`

const defaultGitignoreEntries = "\n# Tusk local index\n.tusk/\n"

func newInitCmd() *cobra.Command {
	var name string

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a Tusk workspace in the current directory",
		Long: `Initialize a Tusk workspace in the current directory.

Creates tusk.toml (the manifest declaring node types and edge types) with a
minimal default schema, bootstraps the SQLite index under .tusk/, and appends
a .tusk/ ignore stanza to .gitignore if one is present.

Safe to run only once per directory: it refuses to overwrite an existing
tusk.toml. After init, edit tusk.toml to declare your node/edge types, then
add content with "tusk node create" or by writing markdown files directly
and running "tusk reindex".`,
		Example: `  # Create a workspace named "my-brain" in the current directory
  tusk init --name my-brain

  # Verify the workspace is healthy
  tusk doctor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, getCwdErr := os.Getwd()

			if getCwdErr != nil {
				return getCwdErr
			}

			manifestPath := filepath.Join(cwd, workspace.ManifestFilename)

			if _, statErr := os.Stat(manifestPath); statErr == nil {
				return fmt.Errorf("init: %s already exists", workspace.ManifestFilename)
			}

			if writeErr := os.WriteFile(manifestPath, []byte(fmt.Sprintf(defaultManifestTemplate, name)), 0o644); writeErr != nil {
				return fmt.Errorf("init: write manifest: %w", writeErr)
			}

			indexPath := filepath.Join(cwd, workspace.IndexDirname, workspace.IndexFilename)

			store, openErr := index.Open(indexPath)

			if openErr != nil {
				return fmt.Errorf("init: bootstrap index: %w", openErr)
			}

			store.Close()

			if appendErr := appendGitignore(filepath.Join(cwd, ".gitignore")); appendErr != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "init: warning: could not update .gitignore: %v\n", appendErr)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Initialized Tusk workspace at %s\n", cwd)

			return nil
		},
	}

	initCmd.Flags().StringVar(&name, "name", "my-brain", "workspace name written into tusk.toml")

	return initCmd
}

// appendGitignore appends Tusk's gitignore stanza if not already present.
// Missing file is fine — a fresh stanza is written.
func appendGitignore(gitignorePath string) error {
	body, readErr := os.ReadFile(gitignorePath)

	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}

	if hasTuskStanza(body) {
		return nil
	}

	updated := append(body, []byte(defaultGitignoreEntries)...)

	return os.WriteFile(gitignorePath, updated, 0o644)
}

func hasTuskStanza(body []byte) bool {
	for _, line := range splitLines(body) {
		if line == ".tusk/" {
			return true
		}
	}

	return false
}

func splitLines(body []byte) []string {
	var lines []string
	var current []byte

	for _, character := range body {
		if character == '\n' {
			lines = append(lines, string(current))
			current = current[:0]

			continue
		}

		current = append(current, character)
	}

	if len(current) > 0 {
		lines = append(lines, string(current))
	}

	return lines
}
