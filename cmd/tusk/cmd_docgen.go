package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/germanamz/tusk/internal/version"
)

const indexHeader = `# Tusk CLI reference

This reference is generated from the Cobra help text in ` + "`cmd/tusk/`" + `.
Edit the help strings, then run ` + "`make docs`" + `.

- **[Workflows](workflows.md)** — multi-command recipes (bootstrap, ingest,
  query, MCP wiring, health checks).

## Commands

`

func newDocgenCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "docgen <man-dir> <md-dir>",
		Short:  "(internal) regenerate man pages and markdown CLI reference",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateDocs(args[0], args[1])
		},
	}
}

func generateDocs(manDir, docDir string) error {
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
		Source:  "Tusk " + version.Current,
		Manual:  "Tusk Manual",
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

	return writeIndex(rootCmd, filepath.Join(docDir, "README.md"))
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

	mdName := strings.ReplaceAll(cmd.CommandPath(), " ", "_") + ".md"

	if _, writeErr := fmt.Fprintf(out, "%s- [`%s`](%s) — %s\n", indent, cmd.CommandPath(), mdName, cmd.Short); writeErr != nil {
		return writeErr
	}

	children := cmd.Commands()

	sort.Slice(children, func(i, j int) bool {
		return children[i].Name() < children[j].Name()
	})

	for _, child := range children {
		if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
			continue
		}

		if writeErr := writeCommandTree(out, child, depth+1); writeErr != nil {
			return writeErr
		}
	}

	return nil
}
