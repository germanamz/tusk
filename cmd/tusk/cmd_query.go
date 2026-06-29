package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/render"
	"github.com/spf13/cobra"
)

func newQueryCmd() *cobra.Command {
	var (
		sortSpec      string
		take          int
		skip          int
		emitJSON      bool
		semanticQuery string
		minScore      float64
		includeFlag   []string
		fieldsFlag    []string
		formatFlag    string

		graphExpand   bool
		graphNoExpand bool
		hops          int
		graphWeight   float64
		graphEdges    []string
		explainFlag   bool
	)

	queryCmd := &cobra.Command{
		Use:   "query <filter>",
		Short: "Run a structural, semantic, or hybrid query against the index",
		Long: `Run a query against the index.

Three modes, all driven by the same command:

  * Structural (default): the filter argument is a property and
    edge-traversal expression. Property predicates use comparison
    operators (key=value, key:value, key!=value, key<value, key<=value,
    key>value, key>=value); ranges use key=lo..hi. Edge traversal uses
    edge-type-> or edge-type<- and may chain multi-hop. Traversal
    shortcuts: tree=id, parent=id, root=id (qualified: tree:<alias>=id,
    parent:<alias>=id, root:<alias>=id, where <alias> is set via
    hierarchy on an edge type in tusk.toml). Recency shortcut:
    modified-since:<duration|ISO-date> (e.g. modified-since:7d,
    modified-since:2026-05-23). Combine with AND, OR, NOT, and parens.
    (Both : and = bind property comparisons; pick whichever reads
    better.)
  * Semantic (--semantic STRING): nearest-neighbor search over
    Ollama embeddings. The positional filter still applies as a
    pre-filter; pass a permissive filter like 'type=note' to search
    a whole type, or '' to search everything.
  * Hybrid: structural filter narrows the candidate set, then
    --semantic ranks it by cosine similarity.

Use --sort to order by one or more keys (prefix +/-), --take N to limit
results, --skip M to paginate. Use --include to expand each row with
body, edges, or properties (comma-separated; for semantic results body
is the best-matching chunk). Use --fields to project the rendered shape.
Use --format to pick compact or JSON output (default: compact for TTY,
JSON otherwise); --json is sugar for --format json.

Sub-unit addresses: a sub-unit's id appends a structural address to the file
id, e.g. notes/doc#S1.2P3 (paragraph 3 of section 1.2) or notes/doc#S1.1T1R0C0
(a table cell). Addresses stay stable under in-place edits and shift only when
the document is restructured.`,
		Example: `  # Structural: all priority-1 tickets touched in the last week
  tusk query 'type=ticket AND priority=1 AND modified-since:7d'

  # Expand bodies and edges in one round-trip
  tusk query 'type=ticket' --include body,edges

  # Pure semantic over all notes
  tusk query 'type=note' --semantic 'cache invalidation strategies'

  # Hybrid: filter to design notes, then rank by similarity
  tusk query 'type=note AND kind=design' --semantic 'sqlite write contention'

  # Pipe top match into "node get"
  tusk query 'type=note' --semantic 'auth flow' --json --take 1 \
    | jq -r '.[0].id' \
    | xargs tusk node get`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace()

			if resolveErr != nil {
				return resolveErr
			}
			// Validate the filter expression before touching the embedder so a
			// malformed filter surfaces before "--semantic requires
			// [embeddings]" — preserves the legacy error-message ordering.
			if validateErr := validateQueryFilter(args[0], loaded); validateErr != nil {
				return validateErr
			}

			embedder, embedErr := buildCLIEmbedder(loaded, semanticQuery)

			if embedErr != nil {
				return embedErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			deps := query.Deps{
				Database:   store.DB(),
				Manifest:   loaded,
				Embedder:   embedder,
				Embeddings: index.NewEmbeddingRepo(store),
				Nodes:      index.NewNodeRepo(store),
				Edges:      index.NewEdgeRepo(store),
			}

			graphExpansion, mergeErr := mergeGraphExpansion(loaded.GraphExpansion, graphExpansionOverrides{
				ExpandSet:   cmd.Flags().Changed("graph-expand"),
				ExpandValue: graphExpand,
				NoExpandSet: cmd.Flags().Changed("no-graph-expand"),
				NoExpand:    graphNoExpand,
				HopsSet:     cmd.Flags().Changed("hops"),
				HopsValue:   hops,
				WeightSet:   cmd.Flags().Changed("graph-weight"),
				WeightValue: graphWeight,
				EdgesSet:    cmd.Flags().Changed("graph-edges"),
				EdgesValue:  graphEdges,
			})

			if mergeErr != nil {
				return mergeErr
			}

			result, runErr := query.Run(context.Background(), deps, query.Request{
				Filter:   args[0],
				Sort:     sortSpec,
				Take:     take,
				Skip:     skip,
				Semantic: semanticQuery,
				MinScore: minScore,
				// CLI preserves the legacy behavior of returning every ranked
				// row when --take is unset; MCP applies a default page size
				// of 10 to keep tool responses bounded.
				SemanticDefaultTake: 0,
				Include:             includeFlag,
				Fields:              fieldsFlag,
				WorkspaceRoot:       ws.Root,
				GraphExpansion:      graphExpansion,
				Explain:             explainFlag,
			})

			if runErr != nil {
				return runErr
			}

			hasShapeFlags := len(includeFlag) > 0 || len(fieldsFlag) > 0
			format, formatErr := resolveFormat(emitJSON, formatFlag, hasShapeFlags)

			if formatErr != nil {
				return formatErr
			}

			if result.Semantic != nil {
				return renderQuerySemantic(cmd, result.Semantic, format, fieldsFlag, explainFlag)
			}

			return renderQueryStructural(cmd, result.Rows, format, fieldsFlag)
		},
	}

	queryCmd.Flags().StringVar(&sortSpec, "sort", "", "sort spec, e.g., +priority,-due,+modified")
	queryCmd.Flags().IntVar(&take, "take", 0, "limit results to N rows")
	queryCmd.Flags().IntVar(&skip, "skip", 0, "skip the first M rows (requires --take)")
	queryCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON (sugar for --format json)")
	queryCmd.Flags().StringVar(&semanticQuery, "semantic", "", "rank results by cosine similarity to this query string (requires [embeddings] in tusk.toml)")
	queryCmd.Flags().Float64Var(&minScore, "min-score", 0, "drop semantic results below this similarity score (default 0 = no filter; MCP tusk_query defaults to 0.5). When graph expansion is active, this filters the blended final score, not the bare cosine.")
	queryCmd.Flags().StringSliceVar(&includeFlag, "include", nil, "expand rows: body|edges|properties|units (comma-separated; units lists each file's sub-units)")
	queryCmd.Flags().StringSliceVar(&fieldsFlag, "fields", nil, "project rendered rows to these fields (comma-separated)")
	queryCmd.Flags().StringVar(&formatFlag, "format", "", "output format: compact|json (default: compact for TTY, json otherwise)")
	queryCmd.Flags().BoolVar(&graphExpand, "graph-expand", false, "enable graph-expanded retrieval for this call (overrides [query.graph-expansion] enabled=false)")
	queryCmd.Flags().BoolVar(&graphNoExpand, "no-graph-expand", false, "disable graph-expanded retrieval for this call (beats [query.graph-expansion] enabled=true)")
	queryCmd.Flags().IntVar(&hops, "hops", 0, "graph-expansion BFS depth (1 or 2; 0 = inherit manifest)")
	queryCmd.Flags().Float64Var(&graphWeight, "graph-weight", -1, "per-hop weight applied to expanded candidates ([0,1]; <0 = inherit manifest)")
	queryCmd.Flags().StringSliceVar(&graphEdges, "graph-edges", nil, "comma-separated edge-type names used by the graph expander; omit to inherit manifest")
	queryCmd.Flags().BoolVar(&explainFlag, "explain", false, "include a per-row score-contribution trace (cosine/graph/final/distance) in the response when graph expansion is active")

	return queryCmd
}

// graphExpansionOverrides captures the per-call CLI flag state for graph
// expansion. Each "Set" boolean is true when cobra reports the flag changed
// from its default; the corresponding value field carries the user input.
type graphExpansionOverrides struct {
	ExpandSet   bool
	ExpandValue bool

	NoExpandSet bool
	NoExpand    bool

	HopsSet   bool
	HopsValue int

	WeightSet   bool
	WeightValue float64

	EdgesSet   bool
	EdgesValue []string
}

// mergeGraphExpansion folds the workspace manifest configuration and the
// per-call CLI overrides into a single resolved GraphExpansion. Precedence
// (high to low): --no-graph-expand → --graph-expand → manifest enabled flag.
// Returns nil only on invalid input (e.g. --graph-weight outside [0,1]).
//
// The returned pointer is never nil on success: subsequent tasks read the
// struct directly off Request.GraphExpansion.
func mergeGraphExpansion(base manifest.GraphExpansion, override graphExpansionOverrides) (*manifest.GraphExpansion, error) {
	resolved := base

	// Struct copy aliases the EdgeTypes slice header; clone the backing
	// array so per-call mutations cannot leak into the shared manifest
	// configuration (matters for MCP, harmless but cheap for CLI).
	if len(base.EdgeTypes) > 0 {
		cloned := make([]string, len(base.EdgeTypes))
		copy(cloned, base.EdgeTypes)
		resolved.EdgeTypes = cloned
	}

	if override.HopsSet {
		if override.HopsValue != 1 && override.HopsValue != 2 {
			return nil, fmt.Errorf("--hops must be 1 or 2 (got %d)", override.HopsValue)
		}

		resolved.Hops = override.HopsValue
	}

	if override.WeightSet {
		if override.WeightValue < 0 || override.WeightValue > 1 {
			return nil, fmt.Errorf("--graph-weight must be in [0.0, 1.0] (got %v)", override.WeightValue)
		}

		resolved.Weight = override.WeightValue
	}

	if override.EdgesSet {
		// Copy to avoid sharing the cobra-owned slice with the manifest.
		edges := make([]string, len(override.EdgesValue))
		copy(edges, override.EdgesValue)

		resolved.EdgeTypes = edges
	}

	// Tri-state precedence: --no-graph-expand beats --graph-expand beats the
	// manifest default. An explicit --graph-expand=false must also override
	// the manifest, so we honor ExpandValue directly when ExpandSet is true
	// instead of gating on the value being true.
	switch {
	case override.NoExpandSet:
		resolved.Enabled = false
	case override.ExpandSet:
		resolved.Enabled = override.ExpandValue
	}

	return &resolved, nil
}

// validateQueryFilter runs the same parse + validate the service does, so the
// CLI can surface filter errors before constructing the embedder. Keeping the
// pre-flight check in cmd/tusk preserves the legacy error-message ordering
// (filter problems beat embeddings problems) without coupling the service to
// CLI presentation concerns.
//
// NOTE: query.Run parses the filter again internally — this pre-flight exists
// solely to surface filter errors before the embedder is constructed,
// preserving pre-refactor CLI error ordering. Do not remove without changing
// query.Run's argument shape (e.g. accepting a pre-parsed Expr instead of a
// raw filter string), otherwise the CLI will start reporting "embeddings
// missing" before "filter parse" again.
func validateQueryFilter(input string, loaded *manifest.Manifest) error {
	expr, parseErrs := filter.NewParser(input).Parse()

	if len(parseErrs) > 0 {
		return fmt.Errorf("filter parse: %v", parseErrs[0])
	}

	if validateErrs := filter.Validate(expr, *loaded); len(validateErrs) > 0 {
		return fmt.Errorf("filter validate: %v", validateErrs[0])
	}

	return nil
}

// buildCLIEmbedder constructs an embed.Embedder from the manifest's
// [embeddings] block when the CLI is invoked with --semantic. Returns nil
// (and nil error) when semanticQuery is empty.
func buildCLIEmbedder(loaded *manifest.Manifest, semanticQuery string) (embed.Embedder, error) {
	if semanticQuery == "" {
		return nil, nil
	}

	if loaded.Embeddings.Provider == "" {
		return nil, fmt.Errorf("--semantic requires [embeddings] block in tusk.toml")
	}

	if loaded.Embeddings.Provider != "ollama" {
		return nil, fmt.Errorf("--semantic: unsupported provider %q (Plan 5 supports ollama only)", loaded.Embeddings.Provider)
	}

	embedder, _ := embed.NewFromManifest(loaded.Embeddings, nil)

	return embedder, nil
}

// renderQueryStructural emits the structural-result rendering. The JSON
// branch emits the rows as a JSON array; the compact branch falls back to
// the legacy tab-aligned table when no include / fields are set so existing
// scripts keep working.
func renderQueryStructural(cmd *cobra.Command, rows []query.Row, format outputFormat, fields []string) error {
	switch format {
	case formatJSON:
		// Preserve the legacy contract: emit `[]\n` when no rows carry
		// expansion data and no fields projection is set. The structural
		// JSON branch used to always emit `[]\n` regardless of content;
		// we tighten that only when the caller asked for expansions so
		// scripts depending on the sentinel still work.
		if len(fields) == 0 && !rowsHaveExpansions(rows) {
			_, _ = cmd.OutOrStdout().Write([]byte("[]\n"))

			return nil
		}

		return writeJSON(cmd.OutOrStdout(), rows)
	case formatCompact:
		return renderStructuralCompact(cmd.OutOrStdout(), rows, fields)
	}

	// formatLegacy: tab-aligned table.
	tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(tab, "ID\tTYPE\tTITLE\tPATH")

	for _, row := range rows {
		_, _ = fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", row.ID, row.Type, row.Title, row.Path)
	}

	return tab.Flush()
}

// rowsHaveExpansions returns true when any row carries body / properties /
// edges populated.
func rowsHaveExpansions(rows []query.Row) bool {
	for _, row := range rows {
		if row.Body != "" || len(row.Properties) > 0 || len(row.Edges) > 0 {
			return true
		}
	}

	return false
}

func renderStructuralCompact(out io.Writer, rows []query.Row, fields []string) error {
	compactRows := make([]render.CompactRow, 0, len(rows))

	for _, row := range rows {
		compactRows = append(compactRows, render.CompactRow{
			ID:           row.ID,
			Type:         row.Type,
			Title:        row.Title,
			Body:         row.Body,
			Properties:   row.Properties,
			Edges:        row.Edges,
			MatchedUnits: row.MatchedUnits,
		})
	}

	return render.CompactNodeRows(out, compactRows, render.CompactOpts{Fields: fields})
}

// renderQuerySemantic emits the ranked-result rendering for --semantic
// queries. JSON output is a flat array of {id,score,type,path,title,snippet,
// body?,properties?,edges?}; the table view shows id/score/snippet; the
// compact view shows the full §4.4 form including expanded body/edges.
func renderQuerySemantic(cmd *cobra.Command, semantic *query.SemanticResult, format outputFormat, fields []string, explain bool) error {
	switch format {
	case formatJSON:
		encoder := json.NewEncoder(cmd.OutOrStdout())

		return encoder.Encode(semantic.Ranked)
	case formatCompact:
		compactRows := make([]render.CompactRow, 0, len(semantic.Ranked))

		for _, scored := range semantic.Ranked {
			compactRows = append(compactRows, render.CompactRow{
				ID:           scored.ID,
				Type:         scored.Type,
				Title:        scored.Title,
				Body:         scored.Body,
				Properties:   scored.Properties,
				Edges:        scored.Edges,
				Score:        scored.Score,
				HasScore:     true,
				MatchedUnits: scored.MatchedUnits,
				CosineScore:  scored.CosineScore,
				GraphScore:   scored.GraphScore,
				FinalScore:   scored.FinalScore,
				Distance:     scored.Distance,
				HasExplain:   explain,
			})
		}

		return render.CompactNodeRows(cmd.OutOrStdout(), compactRows, render.CompactOpts{Fields: fields})
	}

	// formatLegacy: tab-aligned id/score/snippet table.
	tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(tab, "ID\tSCORE\tSNIPPET")

	for _, scored := range semantic.Ranked {
		_, _ = fmt.Fprintf(tab, "%s\t%.4f\t%s\n", scored.ID, scored.Score, scored.Snippet)
	}

	return tab.Flush()
}
