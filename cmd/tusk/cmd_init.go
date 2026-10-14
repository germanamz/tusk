package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/claudeplugin"
	"github.com/germanamz/tusk/internal/ignore"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/typepacks"
	"github.com/germanamz/tusk/internal/version"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

const defaultManifestTemplate = `[workspace]
name = %q
ignore = []
`

// adoptManifestTemplate is the manifest init writes into a folder that
// already holds untyped markdown (#790). The vault pack, appended right after,
// declares the note type default-type names.
const adoptManifestTemplate = `[workspace]
name = %q
ignore = []
# Markdown with no type: of its own is indexed as this type. Remove the line
# to index only files whose frontmatter names a type.
default-type = "note"
`

// adoptPack is the built-in pack init adds when it adopts existing markdown:
// it declares note, plus the wikilink edges that link adopted notes together.
const adoptPack = "vault"

const defaultGitignoreEntries = "\n# Tusk local index\n.tusk/\n"

func newInitCmd() *cobra.Command {
	var (
		name       string
		withClaude bool
		noAdopt    bool
	)

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a Tusk workspace in the current directory",
		Long: `Initialize a Tusk workspace in the current directory, or in the directory
--workspace names.

Creates tusk.toml (the manifest declaring node types and edge types) with a
minimal default schema, bootstraps the SQLite index under .tusk/, and appends
a .tusk/ ignore stanza to .gitignore if one is present.

If the directory already holds markdown without a type: in its frontmatter,
init says how many such files it found, adds the vault pack and sets
[workspace] default-type = "note", so "tusk reindex" indexes them as notes
without editing them. --no-adopt skips this and leaves untyped files out of
the index.

With --claude it also installs the Claude Code plugin, as "tusk claude
install" does.

Safe to run only once per directory: it refuses to overwrite an existing
tusk.toml. After init, edit tusk.toml to declare your node/edge types, then
add content with "tusk node create" or by writing markdown files directly
and running "tusk reindex".`,
		Example: `  # Create a workspace named "my-brain" in the current directory
  tusk init --name my-brain

  # Create it in another folder without changing into it
  tusk init --name notes --workspace ~/notes

  # Index a folder of notes you already have
  cd ~/notes && tusk init --name notes && tusk reindex

  # Leave markdown without a type out of the index
  tusk init --name my-brain --no-adopt

  # Create it and set up Claude Code sessions for it
  tusk init --name my-brain --claude

  # Verify the workspace is healthy
  tusk doctor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, dirErr := startDir(cmd)

			if dirErr != nil {
				return dirErr
			}

			manifestPath := filepath.Join(root, workspace.ManifestFilename)

			if _, statErr := os.Stat(manifestPath); statErr == nil {
				return fmt.Errorf("init: %s already exists", manifestPath)
			}

			untyped := 0

			if !noAdopt {
				count, countErr := countUntypedMarkdown(root)

				if countErr != nil {
					return fmt.Errorf("init: scan for markdown: %w", countErr)
				}

				untyped = count
			}

			template := defaultManifestTemplate

			if untyped > 0 {
				template = adoptManifestTemplate
			}

			if writeErr := os.WriteFile(manifestPath, []byte(fmt.Sprintf(template, name)), 0o644); writeErr != nil {
				return fmt.Errorf("init: write manifest: %w", writeErr)
			}

			if untyped > 0 {
				if addErr := typepacks.AddPack(cmd.Context(), adoptPack, false, root); addErr != nil {
					return fmt.Errorf("init: add the %s pack: %w", adoptPack, addErr)
				}
			}

			indexPath := filepath.Join(root, workspace.IndexDirname, workspace.IndexFilename)

			loaded, loadErr := manifest.Load(manifestPath)

			if loadErr != nil {
				return fmt.Errorf("init: read just-written manifest: %w", loadErr)
			}

			manifest.MergeBuiltinPacks(loaded)

			store, openErr := openStore(cmd, root, indexPath, loaded)

			if openErr != nil {
				return fmt.Errorf("init: bootstrap index: %w", openErr)
			}

			store.Close()

			if appendErr := appendGitignore(filepath.Join(root, ".gitignore")); appendErr != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "init: warning: could not update .gitignore: %v\n", appendErr)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Initialized Tusk workspace at %s\n", root)

			if untyped > 0 {
				printAdoption(cmd, untyped)
			}

			if !withClaude {
				return nil
			}

			return runClaudeInstall(cmd.OutOrStdout(), claudeplugin.InstallOptions{
				Root:    root,
				Bin:     claudeplugin.DefaultBin,
				Version: version.Current,
			})
		},
	}

	initCmd.Flags().StringVar(&name, "name", "my-brain", "workspace name written into tusk.toml")
	initCmd.Flags().BoolVar(&withClaude, "claude", false, "also install the Claude Code plugin (see tusk claude install)")
	initCmd.Flags().BoolVar(&noAdopt, "no-adopt", false, "leave markdown without a type: out of the index instead of indexing it as notes")

	return initCmd
}

// countUntypedMarkdown counts the markdown files under root that reindex would
// skip as not nodes: no frontmatter, or frontmatter without a type. It walks
// what reindex walks, with the same ignore rules and file kinds, so the count
// init reports is the number of notes the first reindex adopts.
func countUntypedMarkdown(root string) (int, error) {
	matcher, matcherErr := ignore.NewMatcher(root, nil)

	if matcherErr != nil {
		return 0, matcherErr
	}

	count := 0

	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, relErr := filepath.Rel(root, path)

		if relErr != nil {
			return relErr
		}

		relPath = filepath.ToSlash(relPath)

		if relPath != "." && matcher.Matches(relPath, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if entry.IsDir() || !index.IsIndexableExt(path) || node.IsHTMLPath(path) || index.ReservedIDReason(relPath) != "" {
			return nil
		}

		content, readErr := os.ReadFile(path)

		if readErr != nil {
			return readErr
		}

		if _, parseErr := node.ParseFile(relPath, content); errors.Is(parseErr, node.ErrMissingFrontmatter) || errors.Is(parseErr, node.ErrMissingType) {
			count++
		}

		return nil
	})

	return count, walkErr
}

// printAdoption tells the user init adopted their untyped markdown and how to
// take it back.
func printAdoption(cmd *cobra.Command, untyped int) {
	out := cmd.OutOrStdout()

	if untyped == 1 {
		_, _ = fmt.Fprintln(out, "Found 1 markdown file without a type: it will be indexed as a note.")
	} else {
		_, _ = fmt.Fprintf(out, "Found %d markdown files without a type: they will be indexed as notes.\n", untyped)
	}

	_, _ = fmt.Fprintf(out, "Added the %s pack and default-type = \"note\" to tusk.toml; remove default-type to index typed files only.\n", adoptPack)
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
