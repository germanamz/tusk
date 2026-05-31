package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/contextcompose"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/render"
)

// newContextCmd builds the `tusk context` Cobra command. It composes the
// warm-context digest declared under [context] in tusk.toml: pinned nodes
// + recent activity + a fan-out over named aliases.
func newContextCmd() *cobra.Command {
	var (
		formatFlag  string
		emitJSON    bool
		includeFlag []string
	)

	contextCmd := &cobra.Command{
		Use:   "context",
		Short: "Compose a warm-context digest from the manifest [context] block",
		Long: `Print the composed warm-context digest for this workspace.

The digest is the single entry point an agent calls at session start instead
of issuing three to five exploratory list/get/query calls. It returns:

  * Pinned nodes ([context.pinned]) with body + edges expanded by default.
  * Recent activity ([context.recent] or recent = "<alias>"): one alias
    result, typically a node list filtered with modified-since:<N>d.
  * Aliases ([context.include]): a fan-out over named, manifest-declared
    aliases; each result is folded under its alias name.

Use --include to override the per-node expansion set for the pinned and
recent sections (default: body,edges). Use --format / --json to override
the output format (default: compact at TTY, JSON when piped).`,
		Example: `  # Pull the digest at session start
  tusk context

  # Trim per-node payload to edges only
  tusk context --include edges

  # Pipe the digest into another tool
  tusk context --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace()

			if resolveErr != nil {
				return resolveErr
			}
			introspect := buildVerbIntrospector(cmd.Root())
			manifest.ValidateAliases(loaded, introspect)
			manifest.ValidateContext(loaded, introspect)

			hasShapeFlags := len(includeFlag) > 0
			format, formatErr := resolveFormat(emitJSON, formatFlag, hasShapeFlags)

			if formatErr != nil {
				return formatErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			aliasDeps := newAliasDeps(store, loaded, ws, buildEmbedder(loaded))

			dispatcher := aliasdispatch.NewDispatcher(aliasDeps)

			composeDeps := contextcompose.Deps{
				Manifest:      loaded,
				Dispatcher:    dispatcher,
				WorkspaceRoot: ws.Root,
				Database:      store.DB(),
			}

			result, composeErr := contextcompose.Compose(cmd.Context(), composeDeps, contextcompose.Request{
				Include: includeFlag,
			})

			if composeErr != nil {
				return composeErr
			}

			return renderContextResult(cmd.OutOrStdout(), result, format)
		},
	}

	contextCmd.Flags().StringVar(&formatFlag, "format", "", "output format: compact|json (default: compact at TTY, JSON when piped)")
	contextCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON (sugar for --format json)")
	contextCmd.Flags().StringSliceVar(&includeFlag, "include", nil, "per-node include set for pinned + recent (default body,edges)")

	return contextCmd
}

// renderContextResult writes the composed digest in either compact or JSON
// form. Compact form sections each section with a Markdown-style header so
// agents can locate them with a regex if needed.
func renderContextResult(out io.Writer, result *contextcompose.Result, format outputFormat) error {
	if format == formatJSON {
		return renderContextJSON(out, result)
	}

	return renderContextCompact(out, result)
}

func renderContextJSON(out io.Writer, result *contextcompose.Result) error {
	payload := buildContextJSONPayload(result)

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	return encoder.Encode(payload)
}

// buildContextJSONPayload converts the typed *Result into the wire envelope
// documented in the Phase 1 Task 5 spec. Exported (lowercase but reused by
// tests in the same package) for parity with the MCP path.
func buildContextJSONPayload(result *contextcompose.Result) map[string]any {
	envelope := map[string]any{}

	if len(result.Pinned) > 0 {
		envelope["pinned"] = result.Pinned
	}

	if len(result.Recent) > 0 {
		envelope["recent"] = result.Recent
	}

	if len(result.Aliases) > 0 {
		aliasEnv := make(map[string]any, len(result.Aliases))

		for _, name := range contextcompose.SortedIncludeNames(result) {
			dispatched := result.Aliases[name]

			aliasEnv[name] = map[string]any{
				"kind":   dispatched.Kind,
				"result": aliasResultPayload(dispatched),
			}
		}

		envelope["aliases"] = aliasEnv
	}

	if len(result.MissingPinned) > 0 {
		envelope["missing_pinned"] = result.MissingPinned
	}

	return envelope
}

func renderContextCompact(out io.Writer, result *contextcompose.Result) error {
	var builder strings.Builder

	if len(result.Pinned) > 0 {
		builder.WriteString("# Pinned\n")

		if renderErr := writeCompactNodeRowsBuilder(&builder, result.Pinned); renderErr != nil {
			return renderErr
		}
	}

	if len(result.Recent) > 0 {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}

		builder.WriteString("# Recent\n")

		if renderErr := writeCompactNodeRowsBuilder(&builder, result.Recent); renderErr != nil {
			return renderErr
		}
	}

	for _, name := range contextcompose.SortedIncludeNames(result) {
		dispatched := result.Aliases[name]

		if builder.Len() > 0 {
			builder.WriteString("\n")
		}

		fmt.Fprintf(&builder, "# Aliases / %s\n", name)

		if renderErr := writeAliasCompactToBuilder(&builder, dispatched); renderErr != nil {
			return renderErr
		}
	}

	if len(result.MissingPinned) > 0 {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}

		builder.WriteString("# Missing pinned\n")

		for _, id := range result.MissingPinned {
			fmt.Fprintf(&builder, "  %s\n", id)
		}
	}

	if builder.Len() == 0 {
		builder.WriteString("tusk context: no [context] block declared in tusk.toml\n")
	}

	_, writeErr := io.WriteString(out, builder.String())

	return writeErr
}

// writeCompactNodeRowsBuilder fans out the standard CompactNodeRows
// renderer into a string builder so each digest section keeps its own
// header without writing intermediate buffers manually.
func writeCompactNodeRowsBuilder(builder *strings.Builder, rows []query.ListRow) error {
	compactRows := make([]render.CompactRow, 0, len(rows))

	for _, row := range rows {
		compactRows = append(compactRows, listRowToCompactBasic(row))
	}

	return render.CompactNodeRows(builder, compactRows, render.CompactOpts{})
}

// writeAliasCompactToBuilder renders a single alias DispatchResult into the
// builder. Reuses renderAlias* helpers from cmd_run.go where possible.
func writeAliasCompactToBuilder(builder *strings.Builder, dispatched *aliasdispatch.DispatchResult) error {
	return renderAliasResult(builder, dispatched, formatCompact)
}
