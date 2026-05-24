// Package query implements the shared structural-and-semantic query verb
// used by both the CLI `tusk query` command and the MCP `tusk_query` tool.
package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

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

	Include       []string
	Fields        []string
	WorkspaceRoot string
}

// Row is a single structural result.
type Row struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	Title         string `json:"title"`
	PropertiesRaw string `json:"-"`

	// Populated only when the request's Include / Fields asked for the
	// matching expansion. Same shape as query.ListRow.
	Body       string         `json:"body,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Edges      []EdgeRef      `json:"edges,omitempty"`
}

// ScoredRow is a single semantic-ranked result.
type ScoredRow struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`

	// Body, when set, is the best-matching chunk body for the query (spec
	// §4.1 — semantic include=body prefers the snippet over the full file).
	Body       string         `json:"body,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Edges      []EdgeRef      `json:"edges,omitempty"`
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
}

// Run is the canonical entry point for the `query` / `tusk_query` verb. It
// parses and compiles the structural filter, runs it against the index, and
// optionally ranks the result by semantic similarity.
//
// Dependencies are passed as primitives to avoid an import cycle on
// internal/mcp.
func Run(ctx context.Context, deps Deps, req Request) (*Result, error) {
	expr, parseErrs := filter.NewParser(req.Filter).Parse()

	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("filter parse: %v", parseErrs[0])
	}

	validateErrs := filter.Validate(expr, *deps.Manifest)

	if len(validateErrs) > 0 {
		return nil, fmt.Errorf("filter validate: %v", validateErrs[0])
	}

	sortKeys, sortErr := filter.ParseSort(req.Sort)

	if sortErr != nil {
		return nil, sortErr
	}

	sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{
		SortKeys: sortKeys,
		Take:     req.Take,
		Skip:     req.Skip,
	})

	if compileErr != nil {
		return nil, compileErr
	}

	rows, queryErr := deps.Database.Query(sqlQuery, params...)

	if queryErr != nil {
		return nil, queryErr
	}

	defer rows.Close()

	var structural []Row
	var nodeIDs []string

	byID := map[string]Row{}

	for rows.Next() {
		var (
			rowID, rowType, rowPath, rowTitle, propertiesRaw, lastChecksum string
			lastMtime, lastSize                                            int64
		)

		if scanErr := rows.Scan(&rowID, &rowType, &rowPath, &rowTitle, &propertiesRaw, &lastMtime, &lastSize, &lastChecksum); scanErr != nil {
			return nil, scanErr
		}

		row := Row{ID: rowID, Type: rowType, Path: rowPath, Title: rowTitle, PropertiesRaw: propertiesRaw}
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

	if req.Semantic == "" {
		if expandErr := ExpandRows(result.Rows, includeSet, req.WorkspaceRoot, deps.Database); expandErr != nil {
			return nil, expandErr
		}

		return result, nil
	}

	if deps.Embedder == nil {
		return nil, fmt.Errorf("semantic ranking requires [embeddings] in tusk.toml")
	}

	queryVector, embedErr := deps.Embedder.Embed(ctx, []byte(req.Semantic))

	if embedErr != nil {
		return nil, embedErr
	}

	loaded, loadErr := deps.Embeddings.ListByNodeIDs(nodeIDs)

	if loadErr != nil {
		return nil, loadErr
	}

	candidates := make([]filter.SemanticCandidate, 0, len(loaded))

	for _, embeddingRow := range loaded {
		candidates = append(candidates, filter.SemanticCandidate{
			NodeID:   embeddingRow.NodeID,
			ChunkIdx: embeddingRow.ChunkIdx,
			Vector:   embeddingRow.Vector,
			Body:     embeddingRow.Body,
		})
	}

	ranked := filter.SemanticRank(candidates, queryVector)

	filteredBelowMinScore := 0

	if req.MinScore > 0 {
		kept := ranked[:0]

		for _, scored := range ranked {
			if scored.Score >= req.MinScore {
				kept = append(kept, scored)

				continue
			}

			filteredBelowMinScore++
		}

		ranked = kept
	}

	effectiveTake := req.Take

	if effectiveTake <= 0 {
		effectiveTake = req.SemanticDefaultTake
	}

	if effectiveTake > 0 {
		startIdx := req.Skip

		if startIdx > len(ranked) {
			startIdx = len(ranked)
		}

		endIdx := startIdx + effectiveTake

		if endIdx > len(ranked) {
			endIdx = len(ranked)
		}

		ranked = ranked[startIdx:endIdx]
	}

	scored := make([]ScoredRow, 0, len(ranked))

	for _, scoredCandidate := range ranked {
		meta := byID[scoredCandidate.NodeID]
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
			var properties map[string]any

			if unmarshalErr := json.Unmarshal([]byte(meta.PropertiesRaw), &properties); unmarshalErr != nil {
				return nil, fmt.Errorf("expand: parse properties for %s: %w", meta.ID, unmarshalErr)
			}

			row.Properties = properties
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
