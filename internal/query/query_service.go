// Package query implements the shared structural-and-semantic query verb
// used by both the CLI `tusk query` command and the MCP `tusk_query` tool.
package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// ErrSemanticUnavailable is returned by Run when a semantic/hybrid query is
// requested but no embedder is configured. It is a sentinel so callers (e.g.
// the graph-view HTTP server) can classify "semantic ranking can't run" via
// errors.Is rather than matching the message text.
var ErrSemanticUnavailable = errors.New("semantic ranking requires [embeddings] in tusk.toml")

// Request configures Run. Filter is the structural filter expression and is
// required (empty string is rejected by the CLI; MCP enforces "filter" as a
// required argument). Semantic, when non-empty, switches to hybrid mode and
// ranks the structural result by cosine similarity to the embedded Semantic
// string.
//
// Include / Fields / WorkspaceRoot mirror ListRequest. For structural rows
// Include populates Body/Edges/Properties via the shared expansion helper.
// For semantic rows Include=body preserves the ranker-chosen Snippet as the
// body (spec §4.1) and never reads the full file from disk.
type Request struct {
	Filter   string
	Sort     string
	Take     int
	Skip     int
	Semantic string
	MinScore float64
	// SemanticDefaultTake is the page size used in semantic mode when Take
	// is 0. The CLI leaves this at 0 (no default — return all ranked rows
	// when --take is unset); the MCP handler sets it to 10 to keep tool
	// responses bounded. Ignored when Take > 0.
	SemanticDefaultTake int

	// StructuralDefaultTake is the row cap applied on the structural (non-
	// semantic) path when Take is 0. The CLI leaves this at 0 (no cap —
	// preserving the legacy "return every matching row" contract and the
	// `--format json` empty-array sentinel); the MCP handler sets it to bound
	// tool responses. Ignored when Take > 0.
	StructuralDefaultTake int

	Include       []string
	Fields        []string
	WorkspaceRoot string

	// GraphExpansion carries the resolved per-call graph-expansion config
	// (defaults merged with manifest [query.graph-expansion] and per-call
	// override flags). Populated by the CLI / MCP handler and read by the
	// semantic retrieval pipeline. Nil is treated as "no expansion".
	GraphExpansion *manifest.GraphExpansion

	// Explain, when true, asks the query service to emit a structured
	// trace of how each result row was scored (semantic + graph
	// contributions).
	Explain bool
}

// Row is a single structural result.
type Row struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	Title         string `json:"title"`
	PropertiesRaw string `json:"-"`

	// ParentID is set for direct sub-unit query results (e.g.
	// `type=section`) so the agent can follow up by file. Empty for file
	// rows. Sourced from the underlying NodeRow.parent_id column.
	ParentID string `json:"parent_id,omitempty"`

	// Populated only when the request's Include / Fields asked for the
	// matching expansion. Same shape as query.ListRow.
	Body       string         `json:"body,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Edges      []EdgeRef      `json:"edges,omitempty"`

	// MatchedUnits is populated when Include contains "units" (structural
	// path) and the workspace has sub-units enabled. Nil otherwise.
	MatchedUnits []MatchedUnit `json:"matched_units,omitempty"`
}

// ScoredRow is a single semantic-ranked result.
type ScoredRow struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`

	// Body, when set, is the best-matching chunk body for the query (spec
	// §4.1 — semantic include=body prefers the snippet over the full file).
	Body       string         `json:"body,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Edges      []EdgeRef      `json:"edges,omitempty"`

	// MatchedUnits, when populated, holds the per-sub-unit hits that
	// contributed to the file-level Score (semantic + sub-units path).
	// Ordered by descending score. Sections are interleaved with leaves
	// per §5.7.
	MatchedUnits []MatchedUnit `json:"matched_units,omitempty"`

	// Explain-only score-trace fields. Populated by the query service only
	// when Request.Explain is true AND graph expansion ran for this row.
	// `omitempty` keeps the JSON wire format byte-stable for callers that
	// never opt into explain mode. When graph expansion is on, Score and
	// FinalScore carry the same value; CosineScore exposes the bare
	// (clipped) cosine and GraphScore the seed-neighbor contribution.
	CosineScore float64 `json:"cosine_score,omitempty"`
	GraphScore  float64 `json:"graph_score,omitempty"`
	FinalScore  float64 `json:"final_score,omitempty"`
	Distance    int     `json:"distance,omitempty"`
}

// Result is the typed payload returned by Run.
//
// When the request had no Semantic string, Rows is populated and Semantic is
// nil. When the request had a Semantic string, Semantic is populated; Rows
// holds the *pre-rank* structural rows (callers that only render Semantic can
// ignore it).
type Result struct {
	Rows []Row

	Semantic *SemanticResult
}

// SemanticResult is the semantic-rank payload. Model is the embedder model
// used. FilteredBelowMinScore counts candidates dropped by MinScore so the
// caller can surface "lower --min-score to see more results" UX.
type SemanticResult struct {
	Ranked                []ScoredRow
	Model                 string
	FilteredBelowMinScore int
}

// Deps bundles the primitives Run needs. Each field is required for the path
// it covers; the optional Embedder / Embeddings fields are only consulted
// when req.Semantic is set.
type Deps struct {
	Database   *sql.DB
	Manifest   *manifest.Manifest
	Embedder   embed.Embedder       // optional; required when req.Semantic != ""
	Embeddings *index.EmbeddingRepo // optional; required when req.Semantic != ""
	// Nodes is the node repository used for sub-unit lookups (matched_units
	// hydration on both the structural include=units path and the semantic
	// grouped-by-parent path). Optional: callers that never request
	// matched_units may leave this nil; the implementation will fall back
	// to a one-off repo when needed.
	Nodes *index.NodeRepo
	// Edges is the edge repository the graph-expansion walker reads from.
	// Optional: required when req.GraphExpansion != nil && Enabled. CLI and
	// MCP both populate this from the shared index store.
	Edges *index.EdgeRepo
}

// Run is the canonical entry point for the `query` / `tusk_query` verb. It
// parses and compiles the structural filter, runs it against the index, and
// optionally ranks the result by semantic similarity.
//
// Dependencies are passed as primitives to avoid an import cycle on
// internal/mcp.
func Run(ctx context.Context, deps Deps, req Request) (*Result, error) {
	// The structural pre-filter feeds two paths. The structural-only path
	// (Semantic == "") returns these rows verbatim, so SQL-level Take/Skip is
	// the final window. The semantic path RE-windows after ranking (see the
	// window() calls below), so applying Take/Skip here would truncate the
	// candidate pool to an arbitrary rowid-ordered slice before scoring —
	// dropping the genuinely top-ranked rows. This is acute when the filter
	// admits sub-unit types (#560): each file contributes many sub-unit rows,
	// so a small LIMIT can slice off whole files before they are ever ranked.
	structTake, structSkip := req.Take, req.Skip

	if req.Semantic != "" {
		structTake, structSkip = 0, 0

		// Semantic re-windows after ranking, so the structural compiler never
		// sees the user's skip and cannot enforce the --skip-requires-take
		// contract. Enforce it here against the effective semantic take so the
		// semantic path errors uniformly (it previously dropped skip silently).
		effectiveSemTake := req.Take

		if effectiveSemTake <= 0 {
			effectiveSemTake = req.SemanticDefaultTake
		}

		if req.Skip > 0 && effectiveSemTake <= 0 {
			return nil, filter.ErrSkipRequiresTake
		}
	} else if structTake <= 0 {
		// Structural-only path: SQL-level Take/Skip is the final window, so
		// apply the caller's default page size. MCP bounds tool output; the CLI
		// leaves StructuralDefaultTake 0 to keep returning every matching row.
		structTake = req.StructuralDefaultTake
	}

	rows, compileErr := compileAndQuery(deps.Database, deps.Manifest, req.Filter, req.Sort, structTake, structSkip)

	if compileErr != nil {
		return nil, compileErr
	}

	defer rows.Close()

	var structural []Row
	var nodeIDs []string

	byID := map[string]Row{}

	for rows.Next() {
		var (
			rowID, rowType, rowPath, rowTitle, propertiesRaw, lastChecksum string
			lastMtime, lastSize                                            int64
			parentID                                                       sql.NullString
		)

		if scanErr := rows.Scan(&rowID, &rowType, &rowPath, &rowTitle, &propertiesRaw, &lastMtime, &lastSize, &lastChecksum, &parentID); scanErr != nil {
			return nil, scanErr
		}

		row := Row{ID: rowID, Type: rowType, Path: rowPath, Title: rowTitle, PropertiesRaw: propertiesRaw}

		if parentID.Valid {
			row.ParentID = parentID.String
		}

		structural = append(structural, row)
		nodeIDs = append(nodeIDs, rowID)
		byID[rowID] = row
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	includeSet, parseIncludeErr := ParseInclude(req.Include)

	if parseIncludeErr != nil {
		return nil, parseIncludeErr
	}

	includeSet = MergeInclude(includeSet, IncludeFromFields(req.Fields))

	result := &Result{Rows: structural}

	subUnitsEnabled := deps.Manifest != nil && deps.Manifest.SubUnitsEnabled()

	if req.Semantic == "" {
		if includeSet.Units && subUnitsEnabled {
			nodes := deps.Nodes

			if nodes == nil {
				return nil, fmt.Errorf("query: include=units requires Nodes in Deps")
			}

			for index := range result.Rows {
				row := &result.Rows[index]

				if row.ParentID != "" {
					// The row itself is a sub-unit (direct sub-unit
					// query). Don't recursively load its sub-tree —
					// the agent asked for that one row.
					continue
				}

				units, loadUnitsErr := LoadFileSubUnits(nodes, row.ID)

				if loadUnitsErr != nil {
					return nil, loadUnitsErr
				}

				row.MatchedUnits = units
			}
		}

		if expandErr := ExpandRows(result.Rows, includeSet, req.WorkspaceRoot, deps.Database); expandErr != nil {
			return nil, expandErr
		}

		return result, nil
	}

	if deps.Embedder == nil {
		return nil, ErrSemanticUnavailable
	}

	queryVector, embedErr := deps.Embedder.Embed(ctx, []byte(req.Semantic))

	if embedErr != nil {
		return nil, embedErr
	}

	if subUnitsEnabled {
		// Try the sub-unit-aware path first. If the workspace has no
		// sub-unit embeddings for any of the candidate files (e.g.
		// callers that wrote file-level embeddings before the sub-unit
		// migration), fall back to the legacy by-node-id flow below so
		// existing fixtures keep working.
		//
		// This is a cheap existence-only probe over the raw structural ids:
		// it returns a boolean without decoding any vector/body payload. The
		// real ranking load inside runSemanticSubUnits queries a DIFFERENT,
		// normalized fileID set, so the probe's result cannot be reused as
		// the ranking input — only as the gate for whether to take this path.
		hasSubEmbeddings, subErr := deps.Embeddings.ExistsSubUnitsForFiles(nodeIDs)

		if subErr != nil {
			return nil, subErr
		}

		if hasSubEmbeddings {
			semanticResult, semanticErr := runSemanticSubUnits(ctx, deps, req, includeSet, queryVector, structural)

			if semanticErr != nil {
				return nil, semanticErr
			}

			result.Semantic = semanticResult

			return result, nil
		}
	}

	loaded, loadErr := deps.Embeddings.ListByNodeIDs(nodeIDs)

	if loadErr != nil {
		return nil, loadErr
	}

	// Only rank vectors stored under the configured model. A vector left behind
	// by a previous [embeddings].model (same dim) would otherwise produce a
	// meaningless cross-model cosine and rank at full confidence; a different
	// dim is dropped by SemanticRank but still silently vanishes the node. Both
	// cases now "drop out of query results" as doctor already assumes, and a
	// reindex --force / reset re-embeds them under the live model (#684).
	queryModel := deps.Embedder.Model()

	candidates := make([]filter.SemanticCandidate, 0, len(loaded))

	for _, embeddingRow := range loaded {
		if embeddingRow.Model != queryModel {
			continue
		}

		candidates = append(candidates, filter.SemanticCandidate{
			NodeID: embeddingRow.NodeID,
			Vector: embeddingRow.Vector,
			Body:   embeddingRow.Body,
		})
	}

	ranked := filter.SemanticRank(candidates, queryVector)

	// graphExpansionActive is also read below to gate the lazy NodeRepo
	// fallback for walked-in neighbors (spec §6.5), so keep it in scope here.
	graphExpansionActive := req.GraphExpansion != nil && req.GraphExpansion.Enabled

	ranked, blendedByID, blendErr := expandAndBlend(ctx, deps, req, ranked)

	if blendErr != nil {
		return nil, blendErr
	}

	ranked, filteredBelowMinScore := applyMinScore(ranked, req.MinScore)

	ranked = window(ranked, req.Skip, req.Take, req.SemanticDefaultTake)

	scored := make([]ScoredRow, 0, len(ranked))

	for _, scoredCandidate := range ranked {
		meta, hasMeta := byID[scoredCandidate.NodeID]

		// Walked neighbors may not be in the structural pre-filter result.
		// Fall back to a lazy NodeRepo lookup so the rendered row still
		// carries title/type/path (spec §6.5 budgets K × avg_degree edge
		// reads; the few extra node reads are negligible).
		if !hasMeta && graphExpansionActive && deps.Nodes != nil {
			nodeRow, getErr := deps.Nodes.Get(scoredCandidate.NodeID)

			if getErr == nil && nodeRow != nil {
				meta = Row{
					ID:            nodeRow.ID,
					Type:          nodeRow.Type,
					Path:          nodeRow.Path,
					Title:         nodeRow.Title,
					PropertiesRaw: nodeRow.PropertiesJSON,
				}
			}
		}

		snippet := filter.RenderSnippetForQuery(scoredCandidate.BestChunkBody, req.Semantic, 200)

		row := ScoredRow{
			ID:      scoredCandidate.NodeID,
			Type:    meta.Type,
			Path:    meta.Path,
			Title:   meta.Title,
			Score:   scoredCandidate.Score,
			Snippet: snippet,
		}

		// Spec §4.1: semantic include=body prefers the best-matching chunk
		// body over the full file body. We use the unrendered chunk body
		// (not the snippet ellipsis) so callers get the full unit.
		if includeSet.Body {
			row.Body = scoredCandidate.BestChunkBody
		}

		if includeSet.Properties && meta.PropertiesRaw != "" {
			properties, propErr := unmarshalProperties(meta.PropertiesRaw, meta.ID)

			if propErr != nil {
				return nil, propErr
			}

			row.Properties = properties
		}

		if req.Explain && blendedByID != nil {
			if trace, ok := blendedByID[scoredCandidate.NodeID]; ok {
				row.CosineScore = trace.Cosine
				row.GraphScore = trace.Graph
				row.FinalScore = trace.Final
				row.Distance = trace.Distance
			}
		}

		scored = append(scored, row)
	}

	if includeSet.Edges {
		if expandErr := expandScoredEdges(scored, deps.Database); expandErr != nil {
			return nil, expandErr
		}
	}

	result.Semantic = &SemanticResult{
		Ranked:                scored,
		Model:                 deps.Embedder.Model(),
		FilteredBelowMinScore: filteredBelowMinScore,
	}

	return result, nil
}

// expandScoredEdges decorates a slice of ScoredRow with edges in a single
// batched SQL round-trip — mirrors loadEdgesForRows for the Row/ListRow shape.
func expandScoredEdges(rows []ScoredRow, db *sql.DB) error {
	if len(rows) == 0 {
		return nil
	}

	likes := make([]rowLike, len(rows))

	for index := range rows {
		likes[index] = &scoredRowLike{row: &rows[index]}
	}

	return loadEdgesForRows(likes, db)
}

// scoredRowLike adapts ScoredRow to rowLike. Only the methods loadEdgesForRows
// actually calls are exercised; body/properties setters are no-ops.
type scoredRowLike struct {
	row *ScoredRow
}

func (adapter *scoredRowLike) rowID() string                { return adapter.row.ID }
func (adapter *scoredRowLike) rowPath() string              { return adapter.row.Path }
func (adapter *scoredRowLike) rowPropertiesRaw() string     { return "" }
func (adapter *scoredRowLike) setBody(string)               {}
func (adapter *scoredRowLike) setProperties(map[string]any) {}
func (adapter *scoredRowLike) setEdges(value []EdgeRef)     { adapter.row.Edges = value }
