package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/germanamz/tusk/internal/filter"
)

// runSemanticSubUnits executes the sub-unit-aware semantic ranking path. It
// is invoked only when the workspace has sub-units enabled. The structural
// pre-filter has already produced `structural` (file rows); this function:
//
//  1. Loads every sub-unit embedding for those files via
//     `ListSubUnitsForFiles` (one batched query).
//  2. Ranks leaves by cosine similarity to queryVector.
//  3. Computes section scores as heading-weight × max(descendant leaf).
//  4. Groups hits by parent file; file score is the max across its hits.
//  5. Applies MinScore / Take / Skip at the file level.
//  6. Hydrates each file's ScoredRow with title/type from the structural
//     pre-filter (cheap byID lookup) and attaches matched_units.
func runSemanticSubUnits(
	_ context.Context,
	deps Deps,
	req Request,
	includeSet IncludeSet,
	queryVector []float32,
	structural []Row,
) (*SemanticResult, error) {
	nodes := deps.Nodes

	if nodes == nil {
		return nil, fmt.Errorf("query: sub-unit semantic ranking requires Nodes in Deps")
	}

	// Index the pre-filter result so we can map a sub-unit's file back to
	// its file-level metadata (type/title/path).
	fileMeta := make(map[string]Row, len(structural))
	fileIDs := make([]string, 0, len(structural))

	for _, row := range structural {
		// Direct sub-unit query: include the row itself rather than
		// recursing. We handle that case in the structural path; here
		// we only expect file rows. Sub-unit rows still get their parent
		// file id recorded so the loop below produces sensible output.
		if row.ParentID != "" {
			// Sub-unit rows leaked into the semantic path. Surface them
			// as standalone ranked rows by treating each sub-unit id as
			// its own "file" for grouping purposes.
			fileMeta[row.ID] = row
			fileIDs = append(fileIDs, row.ID)

			continue
		}

		fileMeta[row.ID] = row
		fileIDs = append(fileIDs, row.ID)
	}

	if len(fileIDs) == 0 {
		return &SemanticResult{Model: deps.Embedder.Model()}, nil
	}

	embeddings, embedErr := deps.Embeddings.ListSubUnitsForFiles(fileIDs)

	if embedErr != nil {
		return nil, embedErr
	}

	// Build the candidate pool from the leaf embeddings only — sections
	// aren't embedded (spec §5.7). The id format `<fileID>#<hash>` lets
	// us bucket later.
	candidates := make([]filter.SemanticCandidate, 0, len(embeddings))

	for _, embeddingRow := range embeddings {
		candidates = append(candidates, filter.SemanticCandidate{
			NodeID:   embeddingRow.NodeID,
			ChunkIdx: embeddingRow.ChunkIdx,
			Vector:   embeddingRow.Vector,
			Body:     embeddingRow.Body,
		})
	}

	ranked := filter.SemanticRank(candidates, queryVector)

	// Apply MinScore at the leaf level so sections only aggregate over
	// passing leaves. The spec is silent on whether MinScore filters
	// sections, but a tighter interpretation (leaves first, then derived
	// section scores) keeps the §5.7 weight semantics simple.
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

	leafScores := make(map[string]float64, len(ranked))
	leafBest := make(map[string]filter.ScoredResult, len(ranked))

	for _, scored := range ranked {
		leafScores[scored.NodeID] = scored.Score
		leafBest[scored.NodeID] = scored
	}

	// Load every sub-unit row for the candidate files in one shot, then
	// build a parent/child index per file for section aggregation. The
	// batched repo call avoids an N+1 sweep over 10-50 candidate files.
	allSubUnits, listErr := nodes.ListSubUnitsForFiles(fileIDs)

	if listErr != nil {
		return nil, listErr
	}

	subIndex := newSubUnitIndex(allSubUnits)

	// Build the matched_units bucket per file. Each leaf becomes a
	// MatchedUnit; each section is aggregated from its descendants. A
	// section with no scored descendants is omitted.
	type fileHit struct {
		matched  []MatchedUnit
		maxScore float64
	}

	hitsByFile := make(map[string]*fileHit, len(fileIDs))

	rememberHit := func(fileID string, unit MatchedUnit) {
		bucket, present := hitsByFile[fileID]

		if !present {
			bucket = &fileHit{}
			hitsByFile[fileID] = bucket
		}

		bucket.matched = append(bucket.matched, unit)

		if unit.HasScore && unit.Score > bucket.maxScore {
			bucket.maxScore = unit.Score
		}
	}

	// Leaf hits.
	for _, scored := range ranked {
		row, ok := subIndex.rowsByID[scored.NodeID]

		if !ok {
			continue
		}

		fileID := fileIDFromSubUnit(scored.NodeID)

		snippet := filter.RenderSnippetForQuery(scored.BestChunkBody, req.Semantic, 200)

		if snippet == "" {
			snippet = filter.RenderSnippet(row.EmbedPayload.String, 200)
		}

		unit := MatchedUnit{
			ID:       row.ID,
			Type:     row.Type,
			Ordinal:  int(row.Ordinal.Int64),
			Score:    scored.Score,
			Snippet:  snippet,
			HasScore: true,
		}

		if row.ParentID.Valid {
			unit.ParentID = row.ParentID.String
		}

		rememberHit(fileID, unit)
	}

	// Section aggregates. Walk every section row, find its descendants'
	// best score among the leaf hits, multiply by the heading weight.
	for _, row := range allSubUnits {
		if row.Type != "section" {
			continue
		}

		leafScore, found := subIndex.bestLeafScoreUnder(row.ID, leafScores)

		if !found {
			continue
		}

		level := readHeadingLevel(row.PropertiesJSON)
		weight := HeadingWeight(level)

		if weight == 0 {
			continue
		}

		fileID := fileIDFromSubUnit(row.ID)

		bestLeafID := subIndex.bestDescendantLeafID(row.ID, leafScores)

		var snippet string

		if best, hasBest := leafBest[bestLeafID]; hasBest {
			snippet = filter.RenderSnippetForQuery(best.BestChunkBody, req.Semantic, 200)
		}

		if snippet == "" {
			snippet = filter.RenderSnippet(subIndex.firstLeafSnippet(row.ID, bestLeafID), 200)
		}

		unit := MatchedUnit{
			ID:           row.ID,
			Type:         "section",
			HeadingLevel: level,
			Ordinal:      int(row.Ordinal.Int64),
			Score:        weight * leafScore,
			Snippet:      snippet,
			HasScore:     true,
		}

		if row.ParentID.Valid {
			unit.ParentID = row.ParentID.String
		}

		rememberHit(fileID, unit)
	}

	// Order files by max score, then by id for determinism. Files that
	// have no hits drop out.
	type fileEntry struct {
		fileID string
		hit    *fileHit
	}

	ordered := make([]fileEntry, 0, len(hitsByFile))

	for fileID, hit := range hitsByFile {
		ordered = append(ordered, fileEntry{fileID: fileID, hit: hit})
	}

	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].hit.maxScore != ordered[right].hit.maxScore {
			return ordered[left].hit.maxScore > ordered[right].hit.maxScore
		}

		return ordered[left].fileID < ordered[right].fileID
	})

	// Apply skip/take at the file level.
	effectiveTake := req.Take

	if effectiveTake <= 0 {
		effectiveTake = req.SemanticDefaultTake
	}

	if effectiveTake > 0 {
		startIdx := req.Skip

		if startIdx > len(ordered) {
			startIdx = len(ordered)
		}

		endIdx := startIdx + effectiveTake

		if endIdx > len(ordered) {
			endIdx = len(ordered)
		}

		ordered = ordered[startIdx:endIdx]
	}

	// Materialize ScoredRow per file. Sort matched_units by descending
	// score, ties broken by ordinal ascending for stable output.
	scoredRows := make([]ScoredRow, 0, len(ordered))

	for _, entry := range ordered {
		meta := fileMeta[entry.fileID]

		sort.SliceStable(entry.hit.matched, func(left, right int) bool {
			if entry.hit.matched[left].Score != entry.hit.matched[right].Score {
				return entry.hit.matched[left].Score > entry.hit.matched[right].Score
			}

			return entry.hit.matched[left].Ordinal < entry.hit.matched[right].Ordinal
		})

		// File-level snippet: first matched unit's snippet (descending
		// score) so the agent sees the strongest hit at a glance.
		topSnippet := ""
		topBody := ""

		if len(entry.hit.matched) > 0 {
			topSnippet = entry.hit.matched[0].Snippet
		}

		// include=body for file rows mirrors today's behavior: the
		// best chunk's body wins. For sub-unit rows we serve the best
		// matched leaf's embed_payload.
		if includeSet.Body && len(entry.hit.matched) > 0 {
			topUnitID := entry.hit.matched[0].ID

			if topUnitID != "" {
				if scored, ok := leafBest[topUnitID]; ok {
					topBody = scored.BestChunkBody
				} else if row, ok := subIndex.rowsByID[topUnitID]; ok {
					// Intentional fallback: when the top matched unit
					// is a section (not a leaf), it has no embedding
					// row in leafBest, so we serve the section's own
					// embed_payload (the heading text) as the body.
					topBody = row.EmbedPayload.String
				}
			}
		}

		row := ScoredRow{
			ID:      entry.fileID,
			Type:    meta.Type,
			Path:    meta.Path,
			Title:   meta.Title,
			Score:   entry.hit.maxScore,
			Snippet: topSnippet,
		}

		if includeSet.Body {
			row.Body = topBody
		}

		if includeSet.Properties && meta.PropertiesRaw != "" {
			var properties map[string]any

			if unmarshalErr := json.Unmarshal([]byte(meta.PropertiesRaw), &properties); unmarshalErr != nil {
				return nil, fmt.Errorf("expand: parse properties for %s: %w", meta.ID, unmarshalErr)
			}

			row.Properties = properties
		}

		row.MatchedUnits = entry.hit.matched

		scoredRows = append(scoredRows, row)
	}

	if includeSet.Edges {
		if expandErr := expandScoredEdges(scoredRows, deps.Database); expandErr != nil {
			return nil, expandErr
		}
	}

	return &SemanticResult{
		Ranked:                scoredRows,
		Model:                 deps.Embedder.Model(),
		FilteredBelowMinScore: filteredBelowMinScore,
	}, nil
}
