package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// Delete removes the file for the given node id, deletes the node row and
// outgoing edges from the index, and leaves incoming edges as dangling
// references (surfaced by tusk doctor in Plan 8).
func Delete(root string, nodeRepo *index.NodeRepo, edgeRepo *index.EdgeRepo, nodeID string) error {
	row, getErr := nodeRepo.Get(nodeID)

	if getErr != nil {
		return getErr
	}

	absPath := filepath.Join(root, row.Path)

	if rmErr := os.Remove(absPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return fmt.Errorf("node: rm %s: %w", absPath, rmErr)
	}

	if deleteEdgesErr := edgeRepo.DeleteBySource(nodeID); deleteEdgesErr != nil {
		return deleteEdgesErr
	}

	if deletePathErr := nodeRepo.DeleteByPath(row.Path); deletePathErr != nil {
		return deletePathErr
	}

	return nil
}

// RenamePlan describes the changes a rename made.
type RenamePlan struct {
	OldID         string
	NewID         string
	OldPath       string
	NewPath       string
	AffectedFiles []string
}

// Rename atomically renames the node file and rewrites every referring edge in
// frontmatter on disk. Index is updated transactionally.
//
// newRelPath is workspace-relative. When the source has a file extension
// (e.g. ".md") but newRelPath does not, the source's extension is inherited so
// the renamed file keeps its on-disk extension and the new node id matches the
// "path without extension" convention used elsewhere.
func Rename(root string, nodeRepo *index.NodeRepo, edgeRepo *index.EdgeRepo, edgeTypes manifest.EdgeTypes, nodeTypes map[string]manifest.NodeType, oldID, newRelPath string) (*RenamePlan, error) {
	row, getErr := nodeRepo.Get(oldID)

	if getErr != nil {
		return nil, getErr
	}

	if filepath.Ext(newRelPath) == "" {
		if sourceExt := filepath.Ext(row.Path); sourceExt != "" {
			newRelPath += sourceExt
		}
	}

	newID := strings.TrimSuffix(newRelPath, filepath.Ext(newRelPath))
	newAbs := filepath.Join(root, newRelPath)

	if _, statErr := os.Stat(newAbs); statErr == nil {
		return nil, fmt.Errorf("node: rename target %s already exists", newRelPath)
	}

	oldAbs := filepath.Join(root, row.Path)

	if mkErr := os.MkdirAll(filepath.Dir(newAbs), 0o755); mkErr != nil {
		return nil, fmt.Errorf("node: mkdir %s: %w", filepath.Dir(newAbs), mkErr)
	}

	if renameFileErr := os.Rename(oldAbs, newAbs); renameFileErr != nil {
		return nil, fmt.Errorf("node: rename file: %w", renameFileErr)
	}

	referring, listErr := edgeRepo.ListByTarget(oldID)

	if listErr != nil {
		_ = os.Rename(newAbs, oldAbs)
		return nil, listErr
	}

	affectedFiles := uniqueSourcePaths(referring)

	for _, sourceFile := range affectedFiles {
		absSource := filepath.Join(root, sourceFile)

		if rewriteErr := rewriteEdgeReferences(absSource, oldID, newID, edgeTypes); rewriteErr != nil {
			return nil, rewriteErr
		}
	}

	// Update node index: delete-old → insert-new (NodeRepo.Upsert is keyed on
	// id; mutating row.ID then upserting would leave the old row behind).
	oldPath := row.Path

	if deleteOldErr := nodeRepo.DeleteByPath(oldPath); deleteOldErr != nil {
		return nil, deleteOldErr
	}

	newRow := *row
	newRow.ID = newID
	newRow.Path = newRelPath

	if upsertErr := nodeRepo.Upsert(newRow); upsertErr != nil {
		return nil, upsertErr
	}

	// Re-base outgoing edges of the renamed node from oldID/oldPath to
	// newID/newRelPath.
	outgoing, listOutErr := edgeRepo.ListBySource(oldID)

	if listOutErr != nil {
		return nil, listOutErr
	}

	rebased := make([]index.EdgeRow, 0, len(outgoing))

	for _, edge := range outgoing {
		rebased = append(rebased, index.EdgeRow{
			Type:       edge.Type,
			SourceID:   newID,
			TargetID:   edge.TargetID,
			SourcePath: newRelPath,
			Kind:       edge.Kind,
			Source:     edge.Source,
		})
	}

	if upsertErr := edgeRepo.UpsertAll(newID, newRelPath, rebased); upsertErr != nil {
		return nil, upsertErr
	}

	if deleteOldEdgesErr := edgeRepo.DeleteBySource(oldID); deleteOldEdgesErr != nil {
		return nil, deleteOldEdgesErr
	}

	// Re-derive each affected file's edges from its (now-rewritten) frontmatter
	// so the index reflects the new target id.
	for _, sourceFile := range affectedFiles {
		content, readErr := os.ReadFile(filepath.Join(root, sourceFile))

		if readErr != nil {
			return nil, fmt.Errorf("node: re-read %s: %w", sourceFile, readErr)
		}

		parsed, parseErr := ParseFile(sourceFile, content)

		if parseErr != nil {
			return nil, parseErr
		}

		if resolveErr := ResolveEdges(parsed, edgeTypes); resolveErr != nil {
			return nil, resolveErr
		}

		if upsertErr := edgeRepo.UpsertAll(parsed.ID, parsed.Path, flattenEdges(parsed, nodeTypes)); upsertErr != nil {
			return nil, upsertErr
		}
	}

	return &RenamePlan{
		OldID:         oldID,
		NewID:         newID,
		OldPath:       oldPath,
		NewPath:       newRelPath,
		AffectedFiles: affectedFiles,
	}, nil
}

func uniqueSourcePaths(edges []index.EdgeRow) []string {
	seen := map[string]struct{}{}
	var ordered []string

	for _, edge := range edges {
		if edge.SourcePath == "" {
			continue
		}

		if _, already := seen[edge.SourcePath]; already {
			continue
		}

		seen[edge.SourcePath] = struct{}{}
		ordered = append(ordered, edge.SourcePath)
	}

	return ordered
}

// rewriteEdgeReferences reads the YAML frontmatter at absPath, replaces every
// scalar / sequence value matching oldID under any declared edge-type key with
// newID, and writes the file back atomically.
func rewriteEdgeReferences(absPath, oldID, newID string, edgeTypes manifest.EdgeTypes) error {
	content, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return fmt.Errorf("node: read %s: %w", absPath, readErr)
	}

	rewritten := rewriteFrontmatterEdgeValues(content, oldID, newID, edgeTypes)

	if writeErr := os.WriteFile(absPath, rewritten, 0o644); writeErr != nil {
		return fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	return nil
}

// rewriteFrontmatterEdgeValues is a line-oriented rewriter. It only touches
// frontmatter lines that start with one of edgeTypes' keys (e.g., "parent: x"
// or "blocks: [a, b]") and only replaces the value when it matches oldID.
//
// Targeted approach (not full YAML round-trip) preserves the user's
// frontmatter formatting.
func rewriteFrontmatterEdgeValues(content []byte, oldID, newID string, edgeTypes manifest.EdgeTypes) []byte {
	lines := strings.Split(string(content), "\n")
	inFrontmatter := false

	for lineIdx, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if inFrontmatter {
				break
			}

			inFrontmatter = true

			continue
		}

		if !inFrontmatter {
			continue
		}

		colonIdx := strings.Index(line, ":")

		if colonIdx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:colonIdx])

		if _, isEdge := edgeTypes[key]; !isEdge {
			continue
		}

		value := line[colonIdx+1:]
		trimmedValue := strings.TrimSpace(value)

		if trimmedValue == oldID {
			lines[lineIdx] = line[:colonIdx+1] + " " + newID

			continue
		}

		if strings.HasPrefix(trimmedValue, "[") && strings.HasSuffix(trimmedValue, "]") {
			inner := trimmedValue[1 : len(trimmedValue)-1]
			parts := strings.Split(inner, ",")

			for partIdx, part := range parts {
				if strings.TrimSpace(part) == oldID {
					parts[partIdx] = " " + newID
				}
			}

			lines[lineIdx] = line[:colonIdx+1] + " [" + strings.Join(parts, ",") + "]"
		}
	}

	return []byte(strings.Join(lines, "\n"))
}
