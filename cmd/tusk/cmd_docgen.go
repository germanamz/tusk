package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// manDate pins the .TH revision date in generated man pages. Without it cobra
// stamps time.Now(), so the committed pages "drift" the moment the calendar
// month rolls over and the docs-drift check fails on unrelated PRs. Bump this
// when you deliberately want the displayed revision date to move.
var manDate = time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)

const indexHeader = `# Tusk CLI reference

This reference is generated from the Cobra help text in ` + "`cmd/tusk/`" + `.
Edit the help strings, then run ` + "`make docs`" + `.

- **[Workflows](workflows.md)** — multi-command recipes (bootstrap, ingest,
  query, MCP wiring, health checks).

## Commands

`

func newDocgenCmd() *cobra.Command {
	var llmsFull string

	docgenCmd := &cobra.Command{
		Use:    "docgen <man-dir> <md-dir>",
		Short:  "(internal) regenerate man pages, the markdown CLI reference and llms-full.txt",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateDocs(args[0], args[1], llmsFull)
		},
	}

	docgenCmd.Flags().StringVar(&llmsFull, "llms-full", "", "also write llms-full.txt here; README.md and docs/guide/ are read from its directory")

	return docgenCmd
}

// generateDocs writes the man pages to manDir and the markdown reference to
// docDir, then, when llmsFullPath is set, llms-full.txt from the fresh tree.
func generateDocs(manDir, docDir, llmsFullPath string) error {
	if mkErr := os.MkdirAll(manDir, 0o755); mkErr != nil {
		return fmt.Errorf("mkdir man: %w", mkErr)
	}

	if mkErr := os.MkdirAll(docDir, 0o755); mkErr != nil {
		return fmt.Errorf("mkdir docs: %w", mkErr)
	}

	rootCmd := newRootCmd()

	rootCmd.DisableAutoGenTag = true

	manHeader := &doc.GenManHeader{
		Title:   "TUSK",
		Section: "1",
		Date:    &manDate,
		// Version-neutral: `make docs` runs the binary without the release
		// ldflags, so version.Current is always "v1.0.0-dev" here. Stamping it
		// would ship a dev version string in every tagged release's man pages;
		// `tusk version` reports the real build version instead.
		Source: "Tusk",
		Manual: "Tusk Manual",
	}

	if manErr := doc.GenManTree(rootCmd, manHeader, manDir); manErr != nil {
		return fmt.Errorf("gen man: %w", manErr)
	}

	linkHandler := func(name string) string {
		return name
	}

	filePrepender := func(filename string) string {
		base := filepath.Base(filename)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		title := strings.ReplaceAll(name, "_", " ")

		return fmt.Sprintf("---\ntitle: %s\n---\n\n", title)
	}

	if mdErr := doc.GenMarkdownTreeCustom(rootCmd, docDir, filePrepender, linkHandler); mdErr != nil {
		return fmt.Errorf("gen markdown: %w", mdErr)
	}

	if indexErr := writeIndex(rootCmd, filepath.Join(docDir, "README.md")); indexErr != nil {
		return indexErr
	}

	if llmsFullPath == "" {
		return nil
	}

	return writeLLMsFull(llmsFullPath, docDir, commandPages(rootCmd))
}

func writeIndex(rootCmd *cobra.Command, indexPath string) error {
	file, createErr := os.Create(indexPath)

	if createErr != nil {
		return fmt.Errorf("create index: %w", createErr)
	}

	defer file.Close()

	if _, writeErr := io.WriteString(file, indexHeader); writeErr != nil {
		return fmt.Errorf("write index header: %w", writeErr)
	}

	return writeCommandTree(file, rootCmd, 0)
}

func writeCommandTree(out io.Writer, cmd *cobra.Command, depth int) error {
	indent := strings.Repeat("  ", depth)

	if _, writeErr := fmt.Fprintf(out, "%s- [`%s`](%s) — %s\n", indent, cmd.CommandPath(), commandPageName(cmd), cmd.Short); writeErr != nil {
		return writeErr
	}

	for _, child := range documentedChildren(cmd) {
		if writeErr := writeCommandTree(out, child, depth+1); writeErr != nil {
			return writeErr
		}
	}

	return nil
}

// commandPages lists the command pages in the order the index lists them:
// depth first, subcommands by name.
func commandPages(cmd *cobra.Command) []string {
	pages := []string{commandPageName(cmd)}

	for _, child := range documentedChildren(cmd) {
		pages = append(pages, commandPages(child)...)
	}

	return pages
}

// commandPageName is the markdown file cobra generates for cmd.
func commandPageName(cmd *cobra.Command) string {
	return strings.ReplaceAll(cmd.CommandPath(), " ", "_") + ".md"
}

// documentedChildren returns cmd's subcommands that get a reference page,
// sorted by name.
func documentedChildren(cmd *cobra.Command) []*cobra.Command {
	var children []*cobra.Command

	for _, child := range cmd.Commands() {
		if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
			continue
		}

		children = append(children, child)
	}

	sort.Slice(children, func(left, right int) bool {
		return children[left].Name() < children[right].Name()
	})

	return children
}
