package query

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
)

// runSemanticSubUnits executes the sub-unit-aware semantic ranking path. It
// is invoked only when the workspace has sub-units enabled. The structural
// pre-filter has produced `structural`; rows are normalized to their parent
// FILE id (a filter may match sub-unit rows directly, e.g. `type=section`),
// then this function runs the spec §5.7 order, with graph expansion injected
// after the leaf cosine rank:
//
//  1. Loads every sub-unit embedding for those files via
//     `ListSubUnitsForFiles` (one batched query).
//  2. Ranks leaves by cosine similarity to queryVector.
//  3. When graph expansion is enabled, walks the leaf id seed set and
//     blends per spec §6.1; the blended FinalScore replaces the bare
//     cosine for both MinScore filtering and the section fold below.
//  4. Filters by MinScore (against the blended score when expansion ran).
//  5. Folds each leaf into its innermost section: one row per section,
//     scored by its best leaf (a leaf before the first heading is its own
//     row).
//  6. Groups hits by parent file; file score is the max across its hits.
//  7. Applies Take / Skip at the file level.
//  8. Hydrates each file's ScoredRow with title/type from the structural
//     pre-filter (cheap byID lookup) and attaches matched_units, capped by
//     MaxUnits / SemanticDefaultMaxUnits with the pre-cap units_total.
func runSemanticSubUnits(
	ctx context.Context,
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
	// its file-level metadata (type/title/path). fileIDs is the deduped,
	// first-seen-ordered set of FILE ids to load sub-unit embeddings for.
	fileMeta := make(map[string]Row, len(structural))
	fileIDs := make([]string, 0, len(structural))
	seen := make(map[string]struct{}, len(structural))

	queue := func(fileID string) {
		if _, ok := seen[fileID]; ok {
			return
		}

		seen[fileID] = struct{}{}
		fileIDs = append(fileIDs, fileID)
	}

	for _, row := range structural {
		if row.ParentID != "" {
			// A sub-unit row leaked into the semantic path because the
			// structural filter matched sub-unit types directly (e.g.
			// `type=section`). Normalize it to its parent file so the
			// parent `<file>#*` glob loads the file's leaves. Treating the
			// sub-unit id as its own "file" would instead glob
			// `<subunit>#*`, which matches nothing — dropping the file (#560).
			queue(fileIDFromSubUnit(row.ID))

			continue
		}

		fileMeta[row.ID] = row
		queue(row.ID)
	}

	if len(fileIDs) == 0 {
		return &SemanticResult{Model: deps.Embedder.Model()}, nil
	}

	// Hydrate metadata for any file that entered ONLY via a leaked sub-unit
	// (its own file row was absent from the structural pre-filter), so the
	// output row still carries type/title/path. A missing parent (orphaned
	// sub-unit) is tolerated: the file's leaves still rank, just without meta.
	for _, fileID := range fileIDs {
		if _, ok := fileMeta[fileID]; ok {
			continue
		}

		nodeRow, getErr := nodes.Get(fileID)

		if errors.Is(getErr, index.ErrNodeNotFound) {
			continue
		}

		if getErr != nil {
			return nil, getErr
		}

		fileMeta[fileID] = Row{
			ID:            nodeRow.ID,
			Type:          nodeRow.Type,
			Path:          nodeRow.Path,
			Title:         nodeRow.Title,
			PropertiesRaw: nodeRow.PropertiesJSON,
		}
	}

	embeddings, embedErr := deps.Embeddings.ListSubUnitsForFiles(fileIDs)

	if embedErr != nil {
		return nil, embedErr
	}

	// Only rank vectors stored under the configured vector key. A leaf left
	// behind by a previous [embeddings].model or num-ctx would otherwise rank on
	// a meaningless cross-model cosine (#684 finding 3); the next reindex
	// detects the settings change and re-embeds it under the live key.
	queryModel := deps.Embedder.VectorKey()

	// Build the candidate pool from the leaf embeddings only — sections
	// aren't embedded (spec §5.7). The id format `<fileID>#<hash>` lets
	// us bucket later. Track which files contributed a live leaf so a file with
	// none can fall back to its file-level vector below.
	candidates := make([]filter.SemanticCandidate, 0, len(embeddings))
	filesWithLiveLeaves := make(map[string]struct{}, len(fileIDs))

	for _, embeddingRow := range embeddings {
		if embeddingRow.Model != queryModel {
			continue
		}

		candidates = append(candidates, filter.SemanticCandidate{
			NodeID: embeddingRow.NodeID,
			Vector: embeddingRow.Vector,
			Body:   embeddingRow.Body,
		})
		filesWithLiveLeaves[fileIDFromSubUnit(embeddingRow.NodeID)] = struct{}{}
	}

	// Per-file fallback (#684 finding 2): a file whose sub-unit leaves are all
	// missing (still draining, evicted) or stale-model still ranks via its own
	// file-level vector, instead of vanishing just because OTHER files in the
	// result set have live leaves. The gate that routed us here is vault-wide;
	// this restores the per-file behavior. Fallback ids carry no '#', so
	// fileIDFromSubUnit maps them to themselves and the bucketing below records a
	// bare file hit (mirroring the legacy file-level semantic path).
	fileLevelCandidateIDs := make(map[string]struct{})

	var fallbackFileIDs []string

	for _, fileID := range fileIDs {
		if _, live := filesWithLiveLeaves[fileID]; !live {
			fallbackFileIDs = append(fallbackFileIDs, fileID)
		}
	}

	if len(fallbackFileIDs) > 0 {
		fileLevel, fileLevelErr := deps.Embeddings.ListByNodeIDs(fallbackFileIDs)

		if fileLevelErr != nil {
			return nil, fileLevelErr
		}

		for _, embeddingRow := range fileLevel {
			if embeddingRow.Model != queryModel {
				continue
			}

			candidates = append(candidates, filter.SemanticCandidate{
				NodeID: embeddingRow.NodeID,
				Vector: embeddingRow.Vector,
				Body:   embeddingRow.Body,
			})
			fileLevelCandidateIDs[embeddingRow.NodeID] = struct{}{}
		}
	}

	ranked := filter.SemanticRank(candidates, queryVector)

	// Graph expansion runs at the FILE level: user-declared edges (wikilink,
	// ref-derived, direct frontmatter) join file ids, so seeding the walker
	// with `file#hash` leaf ids can never match them. The per-file blend is
	// mapped back onto each leaf here — final = (1-w)*leaf_cosine +
	// w*parent_file_graph_score — so MinScore filtering and the section
	// fold below operate on blended scores; walked-in neighbor files
	// (dist > 0 with no ranked leaves) surface as bare rows after the hit
	// bucketing.
	fileBlend, blendErr := expandAndBlendFileLevel(ctx, deps, req, ranked)

	if blendErr != nil {
		return nil, blendErr
	}

	var (
		blendedByID           map[string]blendedTrace
		filesWithRankedLeaves map[string]struct{}
	)

	if fileBlend != nil {
		weight := req.GraphExpansion.Weight
		blendedByID = make(map[string]blendedTrace, len(ranked))
		filesWithRankedLeaves = make(map[string]struct{}, len(ranked))
		kept := ranked[:0]

		for _, leaf := range ranked {
			fileID := fileIDFromSubUnit(leaf.NodeID)
			trace, inPool := fileBlend[fileID]

			if !inPool {
				// The parent file fell outside the seed pool (take *
				// candidate-multiplier) and was not walked in — the same
				// truncation the file-level path applies to its rank tail.
				continue
			}

			filesWithRankedLeaves[fileID] = struct{}{}

			cosine := clipUnitScore(leaf.Score)

			// Parity with the file-level path: a file outside the seed pool
			// holds its position on graph merit alone — the walker zeroes
			// non-seed cosines, so its leaves must not smuggle their cosine
			// back into the blend (that would let rank-tail files leapfrog
			// seeds and contradict the row's own FinalScore trace).
			if trace.Distance > 0 {
				cosine = 0
			}

			final := (1-weight)*cosine + weight*trace.Graph

			leaf.Score = final
			kept = append(kept, leaf)

			blendedByID[leaf.NodeID] = blendedTrace{
				Cosine:   cosine,
				Graph:    trace.Graph,
				Final:    final,
				Distance: trace.Distance,
			}
		}

		ranked = kept
	}

	// Apply MinScore at the leaf level, before leaves fold into their
	// sections, so a section row only ever reports a passing leaf.
	ranked, filteredBelowMinScore := applyMinScore(ranked, req.MinScore)

	leafBest := make(map[string]filter.ScoredResult, len(ranked))

	for _, scored := range ranked {
		leafBest[scored.NodeID] = scored
	}

	// Load every sub-unit row for the candidate files in one shot so each
	// leaf can be resolved to its row and its innermost section. The batched
	// repo call avoids an N+1 sweep over 10-50 candidate files.
	allSubUnits, listErr := nodes.ListSubUnitsForFiles(fileIDs)

	if listErr != nil {
		return nil, listErr
	}

	rowsByID := make(map[string]index.NodeRow, len(allSubUnits))

	for _, row := range allSubUnits {
		rowsByID[row.ID] = row
	}

	// Build the matched_units bucket per file. One finding is one row
	// (#765): each scored leaf folds into its innermost section, which
	// reports the best folded leaf's score and snippet under its own id,
	// heading, and line range. A leaf before the first heading has no
	// section and stays a row of its own. A section with no scored leaf of
	// its own is omitted; its subsections carry their findings.
	type fileHit struct {
		matched  []MatchedUnit
		maxScore float64
		// rowIndex maps a reported row id (section or root leaf) to its
		// position in matched; bestLeaf maps it to the leaf behind its score.
		rowIndex map[string]int
		bestLeaf map[string]string
		// File-level fallback (#684 finding 2): set when the file ranked via its
		// own file-level vector because it had no live sub-unit leaves. Carries
		// the snippet/body of the best file-level chunk; no matched sub-units.
		hasFileLevel     bool
		fileLevelSnippet string
		fileLevelBody    string
	}

	hitsByFile := make(map[string]*fileHit, len(fileIDs))

	bucketFor := func(fileID string) *fileHit {
		bucket, present := hitsByFile[fileID]

		if !present {
			bucket = &fileHit{rowIndex: map[string]int{}, bestLeaf: map[string]string{}}
			hitsByFile[fileID] = bucket
		}

		return bucket
	}

	for _, scored := range ranked {
		if _, isFileLevel := fileLevelCandidateIDs[scored.NodeID]; isFileLevel {
			// File-level fallback candidate: its id is the file id itself, so
			// record a bare file hit scored by the file-level vector (no matched
			// sub-units), mirroring the legacy file-level path (#684 finding 2).
			bucket := bucketFor(scored.NodeID)

			bucket.hasFileLevel = true
			bucket.fileLevelBody = scored.BestChunkBody
			bucket.fileLevelSnippet = filter.RenderSnippetForQuery(scored.BestChunkBody, req.Semantic, 200)

			if scored.Score > bucket.maxScore {
				bucket.maxScore = scored.Score
			}

			continue
		}

		leaf, ok := rowsByID[scored.NodeID]

		if !ok {
			// Walked-in neighbor whose sub-unit row isn't part of the
			// structural pre-filter's file set. Per the Phase 3 plan
			// pitfall ("Sub-unit body lookups for walked neighbors"),
			// the simplest behavior is to drop these rather than do a
			// per-id lookup; the file-level semantic path handles cross-
			// file inclusion via a lazy Nodes.Get fallback.
			continue
		}

		// A leaf's parent is its innermost enclosing section, or the file
		// row itself before the first heading.
		reported := leaf

		if parent, hasParent := rowsByID[leaf.ParentID.String]; leaf.ParentID.Valid && hasParent && parent.Type == "section" {
			reported = parent
		}

		bucket := bucketFor(fileIDFromSubUnit(scored.NodeID))

		if position, seen := bucket.rowIndex[reported.ID]; seen {
			// Keep the best leaf per row; on a tie the earlier leaf in the
			// document wins so the snippet is stable.
			current := bucket.matched[position]
			incumbent := rowsByID[bucket.bestLeaf[reported.ID]]

			if scored.Score < current.Score ||
				(scored.Score == current.Score && leaf.Ordinal.Int64 >= incumbent.Ordinal.Int64) {
				continue
			}
		}

		unit := newMatchedUnit(reported)
		unit.Score = scored.Score
		unit.HasScore = true
		unit.Snippet = filter.RenderSnippetForQuery(scored.BestChunkBody, req.Semantic, 200)

		if unit.Snippet == "" {
			unit.Snippet = filter.RenderSnippet(leaf.EmbedPayload.String, 200)
		}

		if req.Explain && blendedByID != nil {
			if trace, ok := blendedByID[scored.NodeID]; ok {
				unit.CosineScore = trace.Cosine
				unit.GraphScore = trace.Graph
				unit.FinalScore = trace.Final
				unit.Distance = trace.Distance
			}
		}

		if position, seen := bucket.rowIndex[reported.ID]; seen {
			bucket.matched[position] = unit
		} else {
			bucket.rowIndex[reported.ID] = len(bucket.matched)
			bucket.matched = append(bucket.matched, unit)
		}

		bucket.bestLeaf[reported.ID] = scored.NodeID

		if unit.Score > bucket.maxScore {
			bucket.maxScore = unit.Score
		}
	}

	// Surface walked-in neighbor FILES (dist > 0) that produced no leaf hits
	// of their own — nodes with no embeddings, or files outside the
	// structural pre-filter — as bare rows scored by the file-level blend,
	// mirroring the file-level path (spec §6.5). Sub-unit walk candidates
	// were already folded onto their parent file by mergeSubUnitTraces.
	for fileID, trace := range fileBlend {
		if trace.Distance == 0 {
			continue
		}

		if _, hasHits := hitsByFile[fileID]; hasHits {
			continue
		}

		if req.MinScore > 0 && trace.Final < req.MinScore {
			// A walked-in file whose ranked leaves were all MinScore-dropped
			// was already counted per leaf by applyMinScore; counting its
			// bare row again would overstate the recoverable hits.
			if _, counted := filesWithRankedLeaves[fileID]; !counted {
				filteredBelowMinScore++
			}

			continue
		}

		hitsByFile[fileID] = &fileHit{maxScore: trace.Final}
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
	ordered = window(ordered, req.Skip, req.Take, req.SemanticDefaultTake)

	// Materialize ScoredRow per file. Sort matched_units by descending
	// score, ties broken by ordinal ascending for stable output.
	scoredRows := make([]ScoredRow, 0, len(ordered))

	for _, entry := range ordered {
		meta, hasMeta := fileMeta[entry.fileID]

		// Walked-in neighbors may sit outside the structural pre-filter, so
		// fileMeta has no row for them. Fall back to a lazy NodeRepo lookup
		// (spec §6.5) so the rendered row still carries title/type/path,
		// mirroring the file-level path's fallback.
		if !hasMeta && fileBlend != nil {
			nodeRow, getErr := nodes.Get(entry.fileID)

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

		sort.SliceStable(entry.hit.matched, func(left, right int) bool {
			if entry.hit.matched[left].Score != entry.hit.matched[right].Score {
				return entry.hit.matched[left].Score > entry.hit.matched[right].Score
			}

			return entry.hit.matched[left].Ordinal < entry.hit.matched[right].Ordinal
		})

		// File-level snippet: first matched unit's snippet (descending
		// score) so the agent sees the strongest hit at a glance. A file that
		// ranked via its file-level vector (no sub-units) uses that vector's
		// snippet instead (#684 finding 2).
		topSnippet := ""
		topBody := ""

		if len(entry.hit.matched) > 0 {
			topSnippet = entry.hit.matched[0].Snippet
		} else if entry.hit.hasFileLevel {
			topSnippet = entry.hit.fileLevelSnippet
		}

		// include=body for file rows mirrors today's behavior: the
		// best chunk's body wins. For sub-unit rows we serve the chunk of the
		// leaf behind the top row (a section row reports its best leaf, so
		// this is the passage that matched, not the section's whole payload);
		// for a file-level fallback we serve the file-level chunk body.
		if includeSet.Body {
			switch {
			case len(entry.hit.matched) > 0:
				topBody = leafBest[entry.hit.bestLeaf[entry.hit.matched[0].ID]].BestChunkBody
			case entry.hit.hasFileLevel:
				topBody = entry.hit.fileLevelBody
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
			properties, propErr := unmarshalProperties(meta.PropertiesRaw, meta.ID)

			if propErr != nil {
				return nil, propErr
			}

			row.Properties = properties
		}

		// Row-level explain trace from the file-level blend: seeds carry
		// their aggregated cosine at dist 0; walked-in neighbors show the
		// hop distance and graph-derived score that admitted them.
		if req.Explain && fileBlend != nil {
			if trace, ok := fileBlend[entry.fileID]; ok {
				row.CosineScore = trace.Cosine
				row.GraphScore = trace.Graph
				row.FinalScore = trace.Final
				row.Distance = trace.Distance
			}
		}

		row.MatchedUnits, row.UnitsTotal = capUnits(entry.hit.matched, unitsLimit(req.MaxUnits, req.SemanticDefaultMaxUnits))

		scoredRows = append(scoredRows, row)
	}

	if includeSet.Edges {
		if expandErr := expandScoredEdges(scoredRows, deps.Database); expandErr != nil {
			return nil, expandErr
		}
	}

	if includeSet.Paths {
		if attachErr := attachScoredPaths(scoredRows, deps.Database, req); attachErr != nil {
			return nil, attachErr
		}
	}

	return &SemanticResult{
		Ranked:                scoredRows,
		Model:                 deps.Embedder.Model(),
		FilteredBelowMinScore: filteredBelowMinScore,
	}, nil
}
