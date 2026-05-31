package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/render"
	"github.com/germanamz/tusk/internal/status"
)

// newRunCmd builds the `tusk run` Cobra command. The dispatcher is
// constructed lazily inside RunE so cmd_run.go has no init-time dependency
// on a workspace.
func newRunCmd() *cobra.Command {
	var (
		listAliases bool
		formatFlag  string
		emitJSON    bool
	)

	runCmd := &cobra.Command{
		Use:   "run [alias]",
		Short: "Run a manifest-declared alias by name",
		Long: `Invoke an alias declared under [alias.<name>] in tusk.toml.

Aliases are reusable, read-only verb invocations. They bind a command (one
of node list, node get, query, edge list, doctor, status) to a fixed set of
arguments so an agent or operator can dispatch a frequent query by name
rather than retyping the filter / sort / take flags.

Use --list to enumerate the aliases the loaded manifest declares. Use
--format / --json to override the output format (defaults match the
underlying verb: tab-aligned table for the legacy view, compact at TTY,
JSON when piped).`,
		Example: `  # Run a pre-declared alias
  tusk run open-tickets

  # Enumerate every alias the manifest declares
  tusk run --list

  # Force JSON regardless of the alias's default
  tusk run open-tickets --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace()

			if resolveErr != nil {
				return resolveErr
			}
			manifest.ValidateAliases(loaded, buildVerbIntrospector(cmd.Root()))

			if listAliases {
				return printAliasList(cmd.OutOrStdout(), loaded)
			}

			if len(args) != 1 {
				return fmt.Errorf("tusk run: alias name required (or pass --list)")
			}

			aliasName := args[0]
			alias, ok := loaded.Aliases[aliasName]

			if !ok {
				if aliasErr, found := findAliasError(loaded, aliasName); found {
					return fmt.Errorf("alias %q is invalid: %s", aliasName, aliasErr.Message)
				}

				return fmt.Errorf("alias %q not declared in tusk.toml", aliasName)
			}

			format, formatErr := resolveFormat(emitJSON, formatFlag, true)

			if formatErr != nil {
				return formatErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			deps := newAliasDeps(store, loaded, ws, buildEmbedder(loaded))

			dispatcher := aliasdispatch.NewDispatcher(deps)
			result, dispatchErr := dispatcher.Run(context.Background(), alias)

			if dispatchErr != nil {
				return dispatchErr
			}

			return renderAliasResult(cmd.OutOrStdout(), result, format)
		},
	}

	runCmd.Flags().BoolVar(&listAliases, "list", false, "list every alias declared in tusk.toml")
	runCmd.Flags().StringVar(&formatFlag, "format", "", "output format: compact|json (default: matches the verb's TTY behavior)")
	runCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON (sugar for --format json)")

	return runCmd
}

// findAliasError returns the AliasError matching name, if any.
func findAliasError(loaded *manifest.Manifest, name string) (manifest.AliasError, bool) {
	for _, aliasErr := range loaded.AliasErrors {
		if aliasErr.Name == name {
			return aliasErr, true
		}
	}

	return manifest.AliasError{}, false
}

// printAliasList writes a compact "name → command args" table for every
// alias in loaded.Aliases. Errors from AliasErrors are appended afterward
// so the user can see why an alias name is missing. The name/description
// row is rendered through a tabwriter so a vault with many aliases gets
// uniformly-aligned columns.
func printAliasList(out io.Writer, loaded *manifest.Manifest) error {
	names := make([]string, 0, len(loaded.Aliases))

	for name := range loaded.Aliases {
		names = append(names, name)
	}

	sort.Strings(names)

	if len(names) == 0 && len(loaded.AliasErrors) == 0 {
		_, _ = fmt.Fprintln(out, "tusk run: no aliases declared in tusk.toml")

		return nil
	}

	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	for _, name := range names {
		alias := loaded.Aliases[name]
		_, _ = fmt.Fprintf(tab, "%s\t%s\n", name, alias.Description)
		_, _ = fmt.Fprintf(tab, "  → %s", alias.Command)

		argKeys := make([]string, 0, len(alias.Args))

		for key := range alias.Args {
			argKeys = append(argKeys, key)
		}

		sort.Strings(argKeys)

		for _, key := range argKeys {
			_, _ = fmt.Fprintf(tab, " %s=%v", key, alias.Args[key])
		}

		_, _ = fmt.Fprintln(tab)
	}

	if flushErr := tab.Flush(); flushErr != nil {
		return flushErr
	}

	if len(loaded.AliasErrors) > 0 {
		_, _ = fmt.Fprintln(out, "")
		_, _ = fmt.Fprintln(out, "alias errors:")

		for _, aliasErr := range loaded.AliasErrors {
			_, _ = fmt.Fprintf(out, "  %s: %s\n", aliasErr.Name, aliasErr.Message)
		}
	}

	return nil
}

// renderAliasResult dispatches to the verb-appropriate renderer based on
// result.Kind and the chosen format.
func renderAliasResult(out io.Writer, result *aliasdispatch.DispatchResult, format outputFormat) error {
	if format == formatJSON {
		return renderAliasJSON(out, result)
	}

	switch typed := result.Result.(type) {
	case *query.ListResult:
		return renderAliasNodeList(out, typed, format)
	case *query.Result:
		return renderAliasQuery(out, typed, format)
	case *node.GetResult:
		return renderAliasNodeGet(out, typed, format)
	case *index.EdgeListResult:
		return renderAliasEdgeList(out, typed, format)
	case *doctor.Result:
		return renderAliasDoctor(out, typed, format)
	case *status.Result:
		return renderAliasStatus(out, typed, format)
	}

	return fmt.Errorf("renderAliasResult: unknown result type %T for alias %q (kind %q)", result.Result, result.Alias, result.Kind)
}

// renderAliasJSON wraps any DispatchResult into the envelope documented in
// the Phase 1 Task 4 spec: {alias, command, kind, result}.
func renderAliasJSON(out io.Writer, result *aliasdispatch.DispatchResult) error {
	payload := map[string]any{
		"alias":   result.Alias,
		"command": result.Command,
		"kind":    result.Kind,
		"result":  aliasResultPayload(result),
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	return encoder.Encode(payload)
}

// aliasResultPayload converts the typed result into the JSON-friendly shape
// the envelope embeds under "result".
func aliasResultPayload(result *aliasdispatch.DispatchResult) any {
	switch typed := result.Result.(type) {
	case *query.ListResult:
		return map[string]any{
			"rows":  typed.Rows,
			"count": len(typed.Rows),
		}

	case *query.Result:
		if typed.Semantic != nil {
			return map[string]any{
				"results": typed.Semantic.Ranked,
				"count":   len(typed.Semantic.Ranked),
				"model":   typed.Semantic.Model,
			}
		}

		return map[string]any{
			"rows":  typed.Rows,
			"count": len(typed.Rows),
		}

	case *node.GetResult:
		envelope := map[string]any{
			"id":    typed.Node.ID,
			"type":  typed.Node.Type,
			"path":  typed.Node.Path,
			"title": typed.Node.Title,
		}

		if typed.IncludeProperties {
			envelope["properties"] = typed.Node.Properties
		}

		if typed.IncludeEdges {
			envelope["edges"] = typed.Node.Edges
		}

		if typed.IncludeBody {
			envelope["body"] = string(typed.Node.Body)
		}

		return envelope

	case *index.EdgeListResult:
		return map[string]any{
			"rows":  typed.Rows,
			"count": len(typed.Rows),
		}

	case *doctor.Result:
		envelope := map[string]any{
			"issues":              typed.Report.Issues,
			"embed_queue_depth":   typed.Report.EmbedQueueDepth,
			"reindex_queue_depth": typed.Report.ReindexQueueDepth,
		}

		if typed.Migration != nil {
			envelope["migrated"] = typed.Migration.Migrated
			envelope["skipped"] = typed.Migration.Skipped
		}

		return envelope

	case *status.Result:
		return map[string]any{
			"nodes_by_type":       typed.NodesByType,
			"edge_count":          typed.EdgeCount,
			"embed_queue_depth":   typed.EmbedQueueDepth,
			"reindex_queue_depth": typed.ReindexQueueDepth,
			"last_reindex_at":     typed.LastReindexAt,
		}
	}

	return result.Result
}

func renderAliasNodeList(out io.Writer, result *query.ListResult, _ outputFormat) error {
	compactRows := make([]render.CompactRow, 0, len(result.Rows))

	for _, row := range result.Rows {
		compactRows = append(compactRows, listRowToCompactBasic(row))
	}

	return render.CompactNodeRows(out, compactRows, render.CompactOpts{})
}

func renderAliasQuery(out io.Writer, result *query.Result, _ outputFormat) error {
	if result.Semantic != nil {
		compactRows := make([]render.CompactRow, 0, len(result.Semantic.Ranked))

		for _, scored := range result.Semantic.Ranked {
			compactRows = append(compactRows, render.CompactRow{
				ID:         scored.ID,
				Type:       scored.Type,
				Title:      scored.Title,
				Body:       scored.Body,
				Properties: scored.Properties,
				Edges:      scored.Edges,
				Score:      scored.Score,
				HasScore:   true,
			})
		}

		return render.CompactNodeRows(out, compactRows, render.CompactOpts{})
	}

	compactRows := make([]render.CompactRow, 0, len(result.Rows))

	for _, row := range result.Rows {
		compactRows = append(compactRows, render.CompactRow{
			ID:         row.ID,
			Type:       row.Type,
			Title:      row.Title,
			Body:       row.Body,
			Properties: row.Properties,
			Edges:      row.Edges,
		})
	}

	return render.CompactNodeRows(out, compactRows, render.CompactOpts{})
}

func renderAliasNodeGet(out io.Writer, result *node.GetResult, _ outputFormat) error {
	var edgeRefs []query.EdgeRef

	if result.IncludeEdges {
		for edgeType, targets := range result.Node.Edges {
			for _, target := range targets {
				edgeRefs = append(edgeRefs, query.EdgeRef{
					Type:      edgeType,
					Direction: "out",
					TargetID:  target,
				})
			}
		}
	}

	row := render.CompactRow{
		ID:    result.Node.ID,
		Type:  result.Node.Type,
		Title: result.Node.Title,
		Edges: edgeRefs,
	}

	if result.IncludeBody {
		row.Body = string(result.Node.Body)
	}

	if result.IncludeProperties {
		row.Properties = result.Node.Properties
	}

	return render.CompactNodeRows(out, []render.CompactRow{row}, render.CompactOpts{})
}

func renderAliasEdgeList(out io.Writer, result *index.EdgeListResult, _ outputFormat) error {
	entries := make([]render.EdgeListEntry, 0, len(result.Rows))

	for _, row := range result.Rows {
		entries = append(entries, render.EdgeListEntry{
			Type:       row.Type,
			SourceID:   row.SourceID,
			TargetID:   row.TargetID,
			SourcePath: row.SourcePath,
		})
	}

	return render.CompactEdgeRows(out, entries)
}

func renderAliasDoctor(out io.Writer, result *doctor.Result, _ outputFormat) error {
	if len(result.Report.Issues) == 0 {
		_, _ = fmt.Fprintln(out, "doctor: no issues")
	}

	for _, issue := range result.Report.Issues {
		_, _ = fmt.Fprintf(out, "  [%s] %s: %s\n", issue.Kind, issue.NodeID, issue.Message)
	}

	_, _ = fmt.Fprintf(out, "embed queue depth: %d\n", result.Report.EmbedQueueDepth)
	_, _ = fmt.Fprintf(out, "reindex queue depth: %d\n", result.Report.ReindexQueueDepth)

	return nil
}

func renderAliasStatus(out io.Writer, result *status.Result, _ outputFormat) error {
	types := make([]string, 0, len(result.NodesByType))

	for name := range result.NodesByType {
		types = append(types, name)
	}

	sort.Strings(types)

	_, _ = fmt.Fprintln(out, "nodes by type:")

	for _, typeName := range types {
		_, _ = fmt.Fprintf(out, "  %s\t%d\n", typeName, result.NodesByType[typeName])
	}

	_, _ = fmt.Fprintf(out, "edges: %d\n", result.EdgeCount)
	_, _ = fmt.Fprintf(out, "embed queue depth: %d\n", result.EmbedQueueDepth)
	_, _ = fmt.Fprintf(out, "reindex queue depth: %d\n", result.ReindexQueueDepth)
	_, _ = fmt.Fprintf(out, "last reindex at: %s\n", result.LastReindexAt)

	return nil
}
