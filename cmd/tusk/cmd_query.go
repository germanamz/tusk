package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newQueryCmd() *cobra.Command {
	var (
		sortSpec      string
		take          int
		skip          int
		emitJSON      bool
		semanticQuery string
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
    hierarchy on an edge type in tusk.toml). Combine with AND, OR, NOT,
    and parens. (Both : and = bind property comparisons; pick whichever
    reads better.)
  * Semantic (--semantic STRING): nearest-neighbor search over
    Ollama embeddings. The positional filter still applies as a
    pre-filter; pass a permissive filter like 'type=note' to search
    a whole type, or '' to search everything.
  * Hybrid: structural filter narrows the candidate set, then
    --semantic ranks it by cosine similarity.

Use --sort to order by one or more keys (prefix +/-), --take N to limit
results, --skip M to paginate, and --json for machine-readable output.`,
		Example: `  # Structural: all priority-1 tickets touched this week
  tusk query 'type=ticket AND priority=1 AND modified>=2026-05-09'

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
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
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

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			deps := query.Deps{
				Database:   store.DB(),
				Manifest:   loaded,
				Embedder:   embedder,
				Embeddings: index.NewEmbeddingRepo(store),
			}

			result, runErr := query.Run(context.Background(), deps, query.Request{
				Filter:   args[0],
				Sort:     sortSpec,
				Take:     take,
				Skip:     skip,
				Semantic: semanticQuery,
				// CLI preserves the legacy behavior of returning every ranked
				// row when --take is unset; MCP applies a default page size
				// of 10 to keep tool responses bounded.
				SemanticDefaultTake: 0,
			})

			if runErr != nil {
				return runErr
			}

			if result.Semantic != nil {
				return renderQuerySemantic(cmd, result.Semantic, emitJSON)
			}

			return renderQueryStructural(cmd, result.Rows, emitJSON)
		},
	}

	queryCmd.Flags().StringVar(&sortSpec, "sort", "", "sort spec, e.g., +priority,-due,+modified")
	queryCmd.Flags().IntVar(&take, "take", 0, "limit results to N rows")
	queryCmd.Flags().IntVar(&skip, "skip", 0, "skip the first M rows (requires --take)")
	queryCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON")
	queryCmd.Flags().StringVar(&semanticQuery, "semantic", "", "rank results by cosine similarity to this query string (requires [embeddings] in tusk.toml)")

	return queryCmd
}

// validateQueryFilter runs the same parse + validate the service does, so the
// CLI can surface filter errors before constructing the embedder. Keeping the
// pre-flight check in cmd/tusk preserves the legacy error-message ordering
// (filter problems beat embeddings problems) without coupling the service to
// CLI presentation concerns.
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

	timeout := time.Duration(embed.ResolveTimeoutSeconds(loaded.Embeddings.TimeoutSeconds)) * time.Second

	return embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: loaded.Embeddings.Endpoint,
		Model:    loaded.Embeddings.Model,
		Dim:      loaded.Embeddings.Dim,
		Timeout:  timeout,
	}), nil
}

// renderQueryStructural emits the structural-result rendering. The JSON
// branch preserves the legacy behavior of emitting an empty array sentinel
// regardless of result content (no caller depends on the structured body;
// --json is documented as "use --semantic for rich output").
func renderQueryStructural(cmd *cobra.Command, rows []query.Row, emitJSON bool) error {
	if emitJSON {
		_, _ = cmd.OutOrStdout().Write([]byte("[]\n"))

		return nil
	}

	tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(tab, "ID\tTYPE\tTITLE\tPATH")

	for _, row := range rows {
		_, _ = fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", row.ID, row.Type, row.Title, row.Path)
	}

	return tab.Flush()
}

// renderQuerySemantic emits the ranked-result rendering for --semantic
// queries. JSON output is a flat array of {id,score,type,path,title,snippet};
// the table view shows id/score/snippet only.
func renderQuerySemantic(cmd *cobra.Command, semantic *query.SemanticResult, emitJSON bool) error {
	if emitJSON {
		out := make([]map[string]any, 0, len(semantic.Ranked))

		for _, scored := range semantic.Ranked {
			out = append(out, map[string]any{
				"id":      scored.ID,
				"score":   scored.Score,
				"type":    scored.Type,
				"path":    scored.Path,
				"title":   scored.Title,
				"snippet": scored.Snippet,
			})
		}

		encoder := json.NewEncoder(cmd.OutOrStdout())

		return encoder.Encode(out)
	}

	tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(tab, "ID\tSCORE\tSNIPPET")

	for _, scored := range semantic.Ranked {
		_, _ = fmt.Fprintf(tab, "%s\t%.4f\t%s\n", scored.ID, scored.Score, scored.Snippet)
	}

	return tab.Flush()
}
