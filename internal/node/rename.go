package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// Delete removes the file for the given node id, deletes the node row and
// outgoing edges from the index, and leaves incoming edges as dangling
// references (surfaced by tusk doctor in Plan 8).
//
// The file removal goes through WriteWithLease so concurrent writers
// serialize on file_state and the row transitions to state='tombstone'
// as a soft-delete audit record.
func Delete(
	root string,
	nodeRepo *index.NodeRepo,
	edgeRepo *index.EdgeRepo,
	fileState *index.FileStateRepo,
	workerID string,
	leaseTTL time.Duration,
	nodeID string,
) error {
	row, getErr := nodeRepo.Get(nodeID)

	if getErr != nil {
		return getErr
	}

	mutator := func(current []byte) (Mutation, error) {
		if current == nil {
			return WriteNoChange(), nil
		}

		return WriteTombstone(), nil
	}

	if writeErr := WriteWithLease(
		context.Background(), root, fileState, workerID, leaseTTL, row.Path, mutator,
	); writeErr != nil {
		return writeErr
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
//
// Move is the one writer that does NOT route through WriteWithLease: the
// helper is single-path by design (see T4.1's task doc § Scope), and the
// two-path commit here can't be expressed by composing two single-path
// calls. Rename instead composes the lease primitives directly,
// acquiring both source and destination leases in lexicographic order
// of path. The lex ordering is the standard fix for two-lock deadlocks:
// two concurrent moves A→B and B→A would deadlock if each claimed its
// source first, so we force both to claim min(paths) then max(paths).
//
// Frontmatter rewrites on OTHER referring files happen AFTER the two
// leases release. Those sibling files are not under this Rename's
// leases; today the workspace flock serializes all writers so the
// rewrites can't race, but the cross-file rewrite-vs-modify gap is a
// known concern for Phase 5+ when the flock is removed.
func Rename(
	root string,
	nodeRepo *index.NodeRepo,
	edgeRepo *index.EdgeRepo,
	fileState *index.FileStateRepo,
	workerID string,
	leaseTTL time.Duration,
	edgeTypes manifest.EdgeTypes,
	nodeTypes map[string]manifest.NodeType,
	oldID, newRelPath string,
) (*RenamePlan, error) {
	row, getErr := nodeRepo.Get(oldID)

	if getErr != nil {
		return nil, getErr
	}

	if filepath.Ext(newRelPath) == "" {
		if sourceExt := filepath.Ext(row.Path); sourceExt != "" {
			newRelPath += sourceExt
		}
	}

	if localErr := ensureVaultLocal(newRelPath); localErr != nil {
		return nil, localErr
	}

	newID := strings.TrimSuffix(newRelPath, filepath.Ext(newRelPath))
	oldPath := row.Path
	oldAbs := filepath.Join(root, oldPath)
	newAbs := filepath.Join(root, newRelPath)

	if oldPath == newRelPath {
		return nil, fmt.Errorf("node: rename target %s already exists", newRelPath)
	}

	loPath, hiPath := oldPath, newRelPath

	if loPath > hiPath {
		loPath, hiPath = hiPath, loPath
	}

	if ensureErr := fileState.EnsurePlaceholder(loPath); ensureErr != nil {
		return nil, ensureErr
	}

	if ensureErr := fileState.EnsurePlaceholder(hiPath); ensureErr != nil {
		return nil, ensureErr
	}

	if _, claimErr := fileState.Claim(loPath, workerID, leaseTTL); claimErr != nil {
		return nil, claimErr
	}

	if _, claimErr := fileState.Claim(hiPath, workerID, leaseTTL); claimErr != nil {
		_ = releaseAbandon(fileState, loPath, workerID)

		return nil, claimErr
	}

	// Re-check destination-exists INSIDE the held leases so a concurrent
	// Create can't sneak a file in between the check and os.Rename.
	if _, statErr := os.Stat(newAbs); statErr == nil {
		return nil, abandonRenameLeases(fileState, loPath, hiPath, workerID,
			fmt.Errorf("node: rename target %s already exists", newRelPath))
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, abandonRenameLeases(fileState, loPath, hiPath, workerID,
			fmt.Errorf("node: stat %s: %w", newRelPath, statErr))
	}

	srcBytes, readErr := os.ReadFile(oldAbs)

	if readErr != nil {
		return nil, abandonRenameLeases(fileState, loPath, hiPath, workerID,
			fmt.Errorf("node: read %s: %w", oldPath, readErr))
	}

	if mkErr := os.MkdirAll(filepath.Dir(newAbs), 0o755); mkErr != nil {
		return nil, abandonRenameLeases(fileState, loPath, hiPath, workerID,
			fmt.Errorf("node: mkdir %s: %w", filepath.Dir(newAbs), mkErr))
	}

	if renameFileErr := os.Rename(oldAbs, newAbs); renameFileErr != nil {
		return nil, abandonRenameLeases(fileState, loPath, hiPath, workerID,
			fmt.Errorf("node: rename file: %w", renameFileErr))
	}

	dstStat, statErr := os.Stat(newAbs)

	if statErr != nil {
		// Restore the source file so the workspace returns to its
		// pre-Rename shape before we abandon the leases.
		_ = os.Rename(newAbs, oldAbs)

		return nil, abandonRenameLeases(fileState, loPath, hiPath, workerID,
			fmt.Errorf("node: stat %s: %w", newRelPath, statErr))
	}

	newHash := sha256Hex(srcBytes)

	if releaseErr := fileState.Release(index.ReleaseContext{
		Path:        newRelPath,
		WorkerID:    workerID,
		Success:     true,
		State:       index.FileStateLive,
		ContentHash: newHash,
		MtimeNs:     dstStat.ModTime().UnixNano(),
		Size:        dstStat.Size(),
	}); releaseErr != nil {
		return nil, fmt.Errorf("node: rename: release destination %s: %w", newRelPath, releaseErr)
	}

	if releaseErr := fileState.Release(index.ReleaseContext{
		Path:        oldPath,
		WorkerID:    workerID,
		Success:     true,
		State:       index.FileStateTombstone,
		ContentHash: "",
		MtimeNs:     time.Now().UnixNano(),
		Size:        0,
	}); releaseErr != nil {
		return nil, fmt.Errorf("node: rename: release source %s: %w", oldPath, releaseErr)
	}

	referring, listErr := edgeRepo.ListByTarget(oldID)

	if listErr != nil {
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

// releaseAbandon clears a single held lease on the abandon path so
// observed-state columns stay where they were. Errors are swallowed by
// callers that are already returning a more meaningful cause.
func releaseAbandon(repo *index.FileStateRepo, path, workerID string) error {
	return repo.Release(index.ReleaseContext{
		Path:     path,
		WorkerID: workerID,
		Success:  false,
	})
}

// abandonRenameLeases releases BOTH held leases on the abandon path and
// returns cause (wrapped if either release itself failed). Used when an
// error occurs between the two successful Claims and the two commit
// Releases — the file-on-disk state has been rolled back by the caller
// where possible.
func abandonRenameLeases(repo *index.FileStateRepo, loPath, hiPath, workerID string, cause error) error {
	loErr := releaseAbandon(repo, loPath, workerID)
	hiErr := releaseAbandon(repo, hiPath, workerID)

	if loErr != nil {
		return fmt.Errorf("node: rename: abandon %s: %w (cause: %v)", loPath, loErr, cause)
	}

	if hiErr != nil {
		return fmt.Errorf("node: rename: abandon %s: %w (cause: %v)", hiPath, hiErr, cause)
	}

	return cause
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
	inEdgeSequence := false

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

		// A block sequence opened by an edge key: rewrite its "- value"
		// continuation lines until the indentation ends (next key or dedent).
		// This is the shape renderMarkdown emits for any 2+ target edge, so
		// missing it left renamed multi-target edges permanently dangling.
		if inEdgeSequence {
			if isSequenceItem(line) {
				lines[lineIdx] = rewriteSequenceItem(line, oldID, newID)

				continue
			}

			inEdgeSequence = false
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

		// An edge key with no inline value opens a block sequence; its items
		// follow on subsequent indented "- value" lines.
		if trimmedValue == "" {
			inEdgeSequence = true

			continue
		}

		if matchesEdgeTarget(trimmedValue, oldID) {
			lines[lineIdx] = line[:colonIdx+1] + " " + yamlQuoteString(newID)

			continue
		}

		if strings.HasPrefix(trimmedValue, "[") && strings.HasSuffix(trimmedValue, "]") {
			inner := trimmedValue[1 : len(trimmedValue)-1]
			parts := strings.Split(inner, ",")

			for partIdx, part := range parts {
				if matchesEdgeTarget(strings.TrimSpace(part), oldID) {
					parts[partIdx] = " " + newID
				}
			}

			lines[lineIdx] = line[:colonIdx+1] + " [" + strings.Join(parts, ",") + "]"
		}
	}

	return []byte(strings.Join(lines, "\n"))
}

// isSequenceItem reports whether line is a YAML block-sequence item ("- value"),
// ignoring leading indentation.
func isSequenceItem(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")

	return trimmed == "-" || strings.HasPrefix(trimmed, "- ")
}

// rewriteSequenceItem replaces a block-sequence item's value with newID when it
// refers to oldID, preserving the line's indentation and dash.
func rewriteSequenceItem(line, oldID, newID string) string {
	dashIdx := strings.IndexByte(line, '-')

	if dashIdx < 0 {
		return line
	}

	itemValue := strings.TrimSpace(line[dashIdx+1:])

	if !matchesEdgeTarget(itemValue, oldID) {
		return line
	}

	return line[:dashIdx+1] + " " + yamlQuoteString(newID)
}

// matchesEdgeTarget reports whether a frontmatter value — plain or
// YAML-double-quoted as the writer emits — refers to id.
func matchesEdgeTarget(value, id string) bool {
	return yamlUnquoteString(value) == id
}

// yamlUnquoteString reverses yamlQuoteString for double-quoted scalars and
// returns plain scalars unchanged.
func yamlUnquoteString(value string) string {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return value
	}

	inner := value[1 : len(value)-1]

	var builder strings.Builder

	escaped := false

	for charIndex := 0; charIndex < len(inner); charIndex++ {
		char := inner[charIndex]

		if escaped {
			builder.WriteByte(char)

			escaped = false

			continue
		}

		if char == '\\' {
			escaped = true

			continue
		}

		builder.WriteByte(char)
	}

	return builder.String()
}
