package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/claudeplugin"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/pathref"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/workspace"
)

func newClaudeRefsCmd() *cobra.Command {
	var limit int

	refsCmd := &cobra.Command{
		Use:   "refs <path>",
		Short: "Print the reminder the Claude Code plugin adds after an edit",
		Long: `Print the reminder the plugin adds after Claude Code edits a file: the
vault pages that name the file, or a directory above it, in inline code, with
the lines and sections they name it in. The pages that name the file itself
come first, then those naming a nearer directory. At most --max pages are
listed; a last line says how to see the rest.

Pages name paths through edge types that set paths = true in tusk.toml. The
path may be absolute or relative to the current directory; one outside the
workspace prints nothing.

The plugin calls this once per file a session edits. It never fails an edit:
when nothing names the path, the workspace declares no paths edge type, or the
index is missing, it prints nothing. It always exits 0.`,
		Example: `  # Preview the reminder an edit to this file would trigger
  tusk claude refs server/ledger/core/service.go`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClaudeRefs(cmd, args[0], limit)
		},
	}

	refsCmd.Flags().IntVar(&limit, "max", claudeplugin.DefaultMaxRefPages, "list at most N pages")

	return refsCmd
}

func runClaudeRefs(cmd *cobra.Command, rawPath string, limit int) error {
	cwd, cwdErr := os.Getwd()

	if cwdErr != nil {
		return nil
	}

	ws, findErr := workspace.Find(cwd)

	if findErr != nil {
		return nil
	}

	loaded, loadErr := manifest.Load(ws.ManifestPath)

	if loadErr != nil || len(manifest.PathEdgeTypeNames(loaded.EdgeTypes)) == 0 {
		return nil
	}

	relative, inside := workspaceRelative(ws.Root, cwd, rawPath)

	if !inside {
		return nil
	}

	target, clean := pathref.Clean(relative)

	if !clean {
		return nil
	}

	store, _ := openIndexForClaude(ws.IndexPath)

	if store == nil {
		return nil
	}

	defer store.Close()

	pages, lookupErr := pagesNaming(store, target)

	if lookupErr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "tusk claude refs: %v\n", lookupErr)

		return nil
	}

	_, _ = io.WriteString(cmd.OutOrStdout(), claudeplugin.Refs(target, pages, limit))

	return nil
}

// workspaceRelative makes rawPath (absolute, or relative to cwd) relative to
// root with forward slashes. It reports false for a path outside root. A
// symlinked root (macOS /tmp → /private/tmp) is resolved before giving up.
func workspaceRelative(root, cwd, rawPath string) (string, bool) {
	absolute := rawPath

	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(cwd, absolute)
	}

	if relative, inside := relativeInside(root, absolute); inside {
		return relative, true
	}

	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolvedDir, dirErr := filepath.EvalSymlinks(filepath.Dir(absolute))

	if rootErr != nil || dirErr != nil {
		return "", false
	}

	return relativeInside(resolvedRoot, filepath.Join(resolvedDir, filepath.Base(absolute)))
}

func relativeInside(root, absolute string) (string, bool) {
	relative, relErr := filepath.Rel(root, absolute)

	if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}

	return filepath.ToSlash(relative), true
}

// pagesNaming returns the pages whose path refs name target or a directory
// above it, most specific first: pages naming target itself, then those naming
// nearer directories, each group by id. A page naming several of them is listed
// once, under the most specific.
func pagesNaming(store *index.Index, target string) ([]claudeplugin.RefPage, error) {
	ancestors := pathref.Ancestors(target)

	refs, refsErr := index.NewEdgeRepo(store).PathRefsTo(ancestors)

	if refsErr != nil {
		return nil, refsErr
	}

	type pageMatch struct {
		rank  int
		lines []int
	}

	matches := map[string]*pageMatch{}

	for _, ref := range refs {
		rank := slices.Index(ancestors, ref.Target)
		match, seen := matches[ref.SourceID]

		switch {
		case !seen || rank < match.rank:
			matches[ref.SourceID] = &pageMatch{rank: rank}
			match = matches[ref.SourceID]
		case rank > match.rank:
			continue
		}

		if ref.Line > 0 && !slices.Contains(match.lines, ref.Line) {
			match.lines = append(match.lines, ref.Line)
		}
	}

	ids := make([]string, 0, len(matches))

	for id := range matches {
		ids = append(ids, id)
	}

	slices.SortFunc(ids, func(left, right string) int {
		if rankDiff := matches[left].rank - matches[right].rank; rankDiff != 0 {
			return rankDiff
		}

		return strings.Compare(left, right)
	})

	rows, rowsErr := index.NewNodeRepo(store).ListByIDs(ids)

	if rowsErr != nil {
		return nil, rowsErr
	}

	titles := make(map[string]string, len(rows))

	for _, row := range rows {
		titles[row.ID] = row.Title
	}

	pages := make([]claudeplugin.RefPage, 0, len(ids))

	for _, id := range ids {
		match := matches[id]
		where, whereErr := mentionsWhere(store, id, match.lines)

		if whereErr != nil {
			return nil, whereErr
		}

		pages = append(pages, claudeplugin.RefPage{
			ID:    id,
			Title: titles[id],
			Named: ancestors[match.rank],
			Where: where,
		})
	}

	return pages, nil
}

// mentionsWhere renders the lines a page names a path on, each with its
// innermost section's heading.
func mentionsWhere(store *index.Index, pageID string, lines []int) (string, error) {
	if len(lines) == 0 {
		return "", nil
	}

	spans, spansErr := index.SectionSpans(store.DB(), pageID)

	if spansErr != nil {
		return "", spansErr
	}

	slices.Sort(lines)

	mentions := make([]query.PathMention, 0, len(lines))

	for _, line := range lines {
		mention := query.PathMention{Line: line}

		if section, found := index.InnermostSection(spans, line); found {
			mention.Section = section.ID
			mention.Heading = section.Heading
		}

		mentions = append(mentions, mention)
	}

	return query.FormatMentions(mentions), nil
}
