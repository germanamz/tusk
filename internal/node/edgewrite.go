package node

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/manifest"
)

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

		if len(targets) == 1 {
			parsed.Properties[edgeName] = targets[0]
		} else {
			out := make([]any, len(targets))

			for index, target := range targets {
				out[index] = target
			}

			parsed.Properties[edgeName] = out
		}

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

	switch len(kept) {
	case 0:
		delete(parsed.Properties, edgeName)
	case 1:
		parsed.Properties[edgeName] = kept[0]
	default:
		out := make([]any, len(kept))

		for index, target := range kept {
			out[index] = target
		}

		parsed.Properties[edgeName] = out
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
