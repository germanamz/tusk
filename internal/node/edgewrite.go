package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// ErrEdgeTypeNotDeclared is the sentinel Service.AddEdge / Service.RemoveEdge
// return when the edge type is absent from the manifest. Callers match it with
// errors.Is to add context — the MCP tools append a "call tusk_reload" hint
// because their schema is cached, while the CLI (which reloads the manifest each
// run) surfaces it plainly.
var ErrEdgeTypeNotDeclared = errors.New("not declared in manifest")

// AddEdge validates and writes a typed edge from sourceID to targetID, then
// reindexes the source. It owns the full edge-add pipeline — edge-type check,
// source/target node-type allow checks, acyclic cycle detection, frontmatter
// write, reindex — so the CLI `tusk edge add` and the `tusk_edge_add` MCP tool
// reduce to marshal+call+format. The manifest divergence (CLI reloads, MCP
// caches) lives at the call site: each builds a Service with its own edgeTypes /
// nodeTypes, never reloaded here.
func (service *Service) AddEdge(edgeType, sourceID, targetID string) error {
	edgeDef, declared := service.edgeTypes[edgeType]

	if !declared {
		return fmt.Errorf("edge type %q %w", edgeType, ErrEdgeTypeNotDeclared)
	}

	sourceRow, sourceErr := service.repo.Get(sourceID)

	if sourceErr != nil {
		return fmt.Errorf("source: %w", sourceErr)
	}

	if !edgeDef.AllowsSource(sourceRow.Type) {
		return fmt.Errorf("edge type %q does not allow source type %q", edgeType, sourceRow.Type)
	}

	if targetRow, getErr := service.repo.Get(targetID); getErr == nil {
		if !edgeDef.AllowsTarget(targetRow.Type) {
			return fmt.Errorf("edge type %q does not allow target type %q", edgeType, targetRow.Type)
		}
	}

	if edgeDef.Acyclic {
		existing, listErr := service.edges.ListByType(edgeType)

		if listErr != nil {
			return listErr
		}

		if cycleErr := DetectCycle(CycleProbe{EdgeType: edgeType, Source: sourceID, Target: targetID}, edgeAdjacency(existing)); cycleErr != nil {
			return cycleErr
		}
	}

	if writeErr := AddEdgeToFrontmatter(service.root, sourceID, edgeType, targetID, service.edgeTypes); writeErr != nil {
		return writeErr
	}

	return ReindexSource(service.root, service.edges, service.edgeTypes, service.nodeTypes, sourceID)
}

// RemoveEdge removes the (edgeType, sourceID, targetID) edge from the source's
// frontmatter, reindexes, and sweeps any legacy __cli__/__mcp__ sentinel row for
// the same triple. Owns the full edge-remove pipeline so both the CLI and MCP
// handlers reduce to marshal+call+format.
func (service *Service) RemoveEdge(edgeType, sourceID, targetID string) error {
	if _, declared := service.edgeTypes[edgeType]; !declared {
		return fmt.Errorf("edge type %q %w", edgeType, ErrEdgeTypeNotDeclared)
	}

	if writeErr := RemoveEdgeFromFrontmatter(service.root, sourceID, edgeType, targetID, service.edgeTypes); writeErr != nil {
		return writeErr
	}

	if reindexErr := ReindexSource(service.root, service.edges, service.edgeTypes, service.nodeTypes, sourceID); reindexErr != nil {
		return reindexErr
	}

	return service.sweepLegacyEdges(edgeType, sourceID, targetID)
}

// sweepLegacyEdges clears any pre-frontmatter __cli__/__mcp__ sentinel row that
// matches the removed (edgeType, targetID) triple for sourceID, rewriting each
// sentinel source-path with its surviving rows. Idempotent.
func (service *Service) sweepLegacyEdges(edgeType, sourceID, targetID string) error {
	legacy, listErr := service.edges.ListBySource(sourceID)

	if listErr != nil {
		return fmt.Errorf("edge remove: list legacy rows: %w", listErr)
	}

	var keptLegacyCLI, keptLegacyMCP []index.EdgeRow

	for _, row := range legacy {
		matchesTriple := row.Type == edgeType && row.TargetID == targetID

		switch row.SourcePath {
		case index.CLISourcePath:
			if !matchesTriple {
				keptLegacyCLI = append(keptLegacyCLI, row)
			}
		case index.MCPSourcePath:
			if !matchesTriple {
				keptLegacyMCP = append(keptLegacyMCP, row)
			}
		}
	}

	if upsertErr := service.edges.UpsertAll(sourceID, index.CLISourcePath, keptLegacyCLI); upsertErr != nil {
		return upsertErr
	}

	return service.edges.UpsertAll(sourceID, index.MCPSourcePath, keptLegacyMCP)
}

// edgeAdjacency builds a source-id → target-ids adjacency map from edge rows,
// the shape DetectCycle consumes.
func edgeAdjacency(rows []index.EdgeRow) map[string][]string {
	adjacency := map[string][]string{}

	for _, row := range rows {
		adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
	}

	return adjacency
}

// AddEdgeToFrontmatter loads sourceID's markdown file under workspaceRoot,
// inserts targetID under the edge-type key in frontmatter (respecting the
// edge type's cardinality), and atomically rewrites the file.
//
// Cardinality rules:
//   - one-to-one / many-to-one: single target per source. If the key is
//     already present with a different target, return an error. If present
//     with the same target, no-op. Otherwise set the key to the scalar.
//   - one-to-many / many-to-many: list of targets. Append targetID if not
//     already present; preserve insertion order; coalesce scalar->list as
//     needed.
//
// Callers MUST serialize concurrent calls on the same source file (e.g.,
// hold the workspace lock). The function performs an unguarded read-mutate-
// write cycle; concurrent callers would produce a lost-update.
//
// The function does not touch the index — the caller is expected to
// reparse the source file and run an upsert (or rely on the watcher).
// setEdgeTargets writes targets back into props[edgeName]: an empty list deletes
// the key, a single target writes a scalar, and multiple write an ordered []any.
// Insertion order is preserved by ranging targets.
func setEdgeTargets(props map[string]any, edgeName string, targets []string) {
	switch len(targets) {
	case 0:
		delete(props, edgeName)
	case 1:
		props[edgeName] = targets[0]
	default:
		out := make([]any, len(targets))

		for index, target := range targets {
			out[index] = target
		}

		props[edgeName] = out
	}
}

func AddEdgeToFrontmatter(
	workspaceRoot, sourceID, edgeName, targetID string,
	edgeTypes manifest.EdgeTypes,
) error {
	edgeDef, declared := edgeTypes[edgeName]

	if !declared {
		return fmt.Errorf("edgewrite: edge type %q not declared in manifest", edgeName)
	}

	sourcePath := filepath.Join(workspaceRoot, sourceID+".md")

	content, readErr := os.ReadFile(sourcePath)

	if readErr != nil {
		return fmt.Errorf("edgewrite: read %s: %w", sourcePath, readErr)
	}

	parsed, parseErr := ParseFile(sourceID+".md", content)

	if parseErr != nil {
		return fmt.Errorf("edgewrite: parse %s: %w", sourcePath, parseErr)
	}

	existing, present := parsed.Properties[edgeName]

	switch edgeDef.Cardinality {
	case manifest.CardinalityOneToOne, manifest.CardinalityManyToOne:
		if present {
			if existingScalar, isScalar := existing.(string); isScalar && existingScalar == targetID {
				return nil // idempotent
			}

			return fmt.Errorf(
				"edgewrite: edge %q on %s already targets %v; remove the existing edge first",
				edgeName, sourceID, existing,
			)
		}

		parsed.Properties[edgeName] = targetID

	case manifest.CardinalityOneToMany, manifest.CardinalityManyToMany:
		targets := toTargetList(existing)

		for _, candidate := range targets {
			if candidate == targetID {
				return nil // idempotent
			}
		}

		targets = append(targets, targetID)

		setEdgeTargets(parsed.Properties, edgeName, targets)

	default:
		return fmt.Errorf("edgewrite: unknown cardinality %q on edge %q", edgeDef.Cardinality, edgeName)
	}

	rendered, renderErr := renderMarkdown(parsed.Properties, parsed.Body)

	if renderErr != nil {
		return fmt.Errorf("edgewrite: render %s: %w", sourcePath, renderErr)
	}

	if writeErr := atomicWrite(sourcePath, rendered); writeErr != nil {
		return fmt.Errorf("edgewrite: write %s: %w", sourcePath, writeErr)
	}

	return nil
}

// RemoveEdgeFromFrontmatter removes targetID from the edge-name key on
// sourceID's frontmatter. Idempotent: succeeds with no-op if the edge is
// not present. Removes the key entirely when the last target is removed.
//
// Callers MUST serialize concurrent calls on the same source file (e.g.,
// hold the workspace lock). The function performs an unguarded read-mutate-
// write cycle; concurrent callers would produce a lost-update.
func RemoveEdgeFromFrontmatter(
	workspaceRoot, sourceID, edgeName, targetID string,
	edgeTypes manifest.EdgeTypes,
) error {
	if _, declared := edgeTypes[edgeName]; !declared {
		return fmt.Errorf("edgewrite: edge type %q not declared in manifest", edgeName)
	}

	sourcePath := filepath.Join(workspaceRoot, sourceID+".md")

	content, readErr := os.ReadFile(sourcePath)

	if readErr != nil {
		return fmt.Errorf("edgewrite: read %s: %w", sourcePath, readErr)
	}

	parsed, parseErr := ParseFile(sourceID+".md", content)

	if parseErr != nil {
		return fmt.Errorf("edgewrite: parse %s: %w", sourcePath, parseErr)
	}

	existing, present := parsed.Properties[edgeName]

	if !present {
		return nil // idempotent
	}

	targets := toTargetList(existing)
	kept := make([]string, 0, len(targets))
	removed := false

	for _, candidate := range targets {
		if candidate == targetID {
			removed = true
			continue
		}

		kept = append(kept, candidate)
	}

	if !removed {
		return nil // idempotent
	}

	setEdgeTargets(parsed.Properties, edgeName, kept)

	rendered, renderErr := renderMarkdown(parsed.Properties, parsed.Body)

	if renderErr != nil {
		return fmt.Errorf("edgewrite: render %s: %w", sourcePath, renderErr)
	}

	if writeErr := atomicWrite(sourcePath, rendered); writeErr != nil {
		return fmt.Errorf("edgewrite: write %s: %w", sourcePath, writeErr)
	}

	return nil
}

// ReindexSource re-reads sourceID's markdown file under workspaceRoot,
// parses + resolves edges, and upserts the resulting edge rows into the
// index under the source's real path. Designed for callers that just
// rewrote the source file via AddEdgeToFrontmatter / RemoveEdgeFromFrontmatter
// and need the index to reflect the new edges before the call returns.
//
// Callers MUST hold the workspace lock; this performs I/O and an index
// write that overlap with the lock contract of the calling command.
func ReindexSource(
	workspaceRoot string,
	edges *index.EdgeRepo,
	edgeTypes manifest.EdgeTypes,
	nodeTypes map[string]manifest.NodeType,
	sourceID string,
) error {
	relPath := sourceID + ".md"
	absPath := filepath.Join(workspaceRoot, relPath)

	content, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return fmt.Errorf("edgewrite: read %s: %w", relPath, readErr)
	}

	parsed, parseErr := ParseFile(relPath, content)

	if parseErr != nil {
		return fmt.Errorf("edgewrite: parse %s: %w", relPath, parseErr)
	}

	if resolveErr := ResolveEdges(parsed, edgeTypes); resolveErr != nil {
		return fmt.Errorf("edgewrite: resolve %s: %w", relPath, resolveErr)
	}

	if upsertErr := edges.UpsertAll(parsed.ID, parsed.Path, flattenEdges(parsed, nodeTypes)); upsertErr != nil {
		return fmt.Errorf("edgewrite: upsert %s: %w", relPath, upsertErr)
	}

	return nil
}

// toTargetList coalesces a frontmatter edge value (scalar string or list of
// strings) into a slice of target IDs. Unknown shapes return nil.
func toTargetList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))

		for _, element := range typed {
			if s, isString := element.(string); isString {
				out = append(out, s)
			}
		}

		return out
	}

	return nil
}
