package bookview

import (
	"context"
	"database/sql"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
)

// searchGraphExpansionLabels names the search endpoint's graph-expansion knobs
// in manifest.MergeGraphExpansion's validation messages: "hops must be 1 or 2
// (got 3)". Chosen to match the wire contract's own JSON field names
// (SearchRequest.Hops/Weight) rather than the CLI's dashed flags or MCP's
// trailing-colon argument names — this is the wording an HTTP JSON API
// consumer (a TypeScript frontend) will see verbatim in an error response.
var searchGraphExpansionLabels = manifest.GraphExpansionLabels{Hops: "hops", Weight: "weight"}

// searcher adapts query.Run to the Searcher interface: it runs a structural
// filter, optionally ranks it by semantic similarity to req.Q, and optionally
// blends in graph-expansion neighbors. Built by NewSearcher in the command
// layer, over an already-open workspace's dependencies.
type searcher struct {
	deps          query.Deps
	workspace     *manifest.Manifest
	workspaceRoot string
}

// NewSearcher builds the real Searcher, wrapping query.Run over an
// already-open workspace's dependencies. db, embedder, embeddings, nodes, and
// edges are handed straight to query.Deps; workspaceManifest supplies both
// query.Deps.Manifest and the [query.graph-expansion] default that per-request
// Hops/EdgeTypes/Weight overrides merge onto. root becomes
// query.Request.WorkspaceRoot for every call.
func NewSearcher(
	db *sql.DB,
	workspaceManifest *manifest.Manifest,
	embedder embed.Embedder,
	embeddings *index.EmbeddingRepo,
	nodes *index.NodeRepo,
	edges *index.EdgeRepo,
	root string,
) Searcher {
	return &searcher{
		deps: query.Deps{
			Database:   db,
			Manifest:   workspaceManifest,
			Embedder:   embedder,
			Embeddings: embeddings,
			Nodes:      nodes,
			Edges:      edges,
		},
		workspace:     workspaceManifest,
		workspaceRoot: root,
	}
}

// Search implements Searcher. The returned error is query.Run's (or
// manifest.MergeGraphExpansion's) raw error, unwrapped, so handleSearch can
// classify it by identity (errors.Is / embed.IsTransportError) rather than
// message text.
func (adapter *searcher) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	qreq := query.Request{
		Filter: req.Filter,
		// Semantic empty means the structural-only path. SemanticDefaultTake
		// and StructuralDefaultTake are two independent knobs, each consulted
		// on only one of the two paths — set both to req.Limit or a
		// structural-only request with Take==0 (Limit unset) would silently
		// ignore Limit and fall back to query's own built-in default.
		Semantic:              req.Q,
		Take:                  req.Limit,
		SemanticDefaultTake:   req.Limit,
		StructuralDefaultTake: req.Limit,
		WorkspaceRoot:         adapter.workspaceRoot,
		Explain:               req.Explain,
	}

	if req.Expand {
		graphExpansion, mergeErr := manifest.MergeGraphExpansion(adapter.workspace.GraphExpansion, graphExpansionOverridesFromSearch(req))

		if mergeErr != nil {
			return SearchResponse{}, mergeErr
		}

		qreq.GraphExpansion = graphExpansion
	}

	result, runErr := query.Run(ctx, adapter.deps, qreq)

	if runErr != nil {
		return SearchResponse{}, runErr
	}

	return searchResponseFrom(req, qreq, result), nil
}

// graphExpansionOverridesFromSearch converts a SearchRequest's plain
// Hops/Weight/EdgeTypes into the presence-aware pointers
// manifest.MergeGraphExpansion needs.
//
// SearchRequest carries plain int/float64 fields (Task 2.1 fixed that wire
// shape; a TypeScript frontend already codes against it), so an omitted JSON
// field and an explicit zero arrive identically as Go's zero value. This
// adapter's presence rule treats 0 as "not specified" for both Hops and
// Weight: an absent hops/weight must inherit the manifest's configured
// default rather than overwrite it with 0, which for Weight would silently
// zero every distance-2 graph-expansion score (the withdrawn brief guard's
// failure mode). The tradeoff — an explicit hops=0 or weight=0 cannot be
// requested through this endpoint — mirrors the CLI's own
// --graph-weight "inherit" sentinel default.
//
// EdgeTypes needs no such treatment: JSON decodes an absent/null
// "edge_types" into a nil slice and only produces a non-nil (possibly empty)
// slice when the field was actually present in the body, so nil already
// means absent.
func graphExpansionOverridesFromSearch(req SearchRequest) manifest.GraphExpansionOverrides {
	// req.Expand is the endpoint's own on/off switch — the caller (gated by
	// the "if req.Expand" check in Search) explicitly asked to expand this
	// search, so the override always forces Enabled rather than inheriting
	// the manifest's base value.
	enabled := true

	over := manifest.GraphExpansionOverrides{
		Enabled: &enabled,
		Labels:  searchGraphExpansionLabels,
	}

	if req.Hops != 0 {
		hops := req.Hops
		over.Hops = &hops
	}

	if req.Weight != 0 {
		weight := req.Weight
		over.Weight = &weight
	}

	if req.EdgeTypes != nil {
		edgeTypes := req.EdgeTypes
		over.EdgeTypes = &edgeTypes
	}

	return over
}

// searchResponseFrom projects a query.Result into the wire SearchResponse.
// Matches is always a non-nil, possibly empty slice — the wire contract is
// [], never null.
//
// query_service.go populates ScoredRow's explain fields (CosineScore,
// GraphScore, FinalScore, Distance) only when Request.Explain is true AND
// graph expansion produced a trace for the row. The copy here is gated on
// both req.Explain and expand explicitly, rather than relying on the source
// fields happening to already be zero when Explain was false.
func searchResponseFrom(req SearchRequest, qreq query.Request, result *query.Result) SearchResponse {
	if req.Q != "" && result.Semantic != nil {
		expand := qreq.GraphExpansion != nil && qreq.GraphExpansion.Enabled
		explainActive := req.Explain && expand

		matches := make([]Match, 0, len(result.Semantic.Ranked))

		for _, ranked := range result.Semantic.Ranked {
			match := Match{ID: ranked.ID, Title: ranked.Title, Type: ranked.Type, Score: ranked.Score}

			if explainActive {
				match.CosineScore = ranked.CosineScore
				match.GraphScore = ranked.GraphScore
				match.FinalScore = ranked.FinalScore
				match.Distance = ranked.Distance
			}

			matches = append(matches, match)
		}

		return SearchResponse{Matches: matches, Model: result.Semantic.Model}
	}

	matches := make([]Match, 0, len(result.Rows))

	for _, row := range result.Rows {
		matches = append(matches, Match{ID: row.ID, Title: row.Title, Type: row.Type, Score: 1})
	}

	return SearchResponse{Matches: matches}
}
