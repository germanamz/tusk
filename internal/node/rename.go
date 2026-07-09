package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

	// MtimeNs is deliberately NOT the on-disk mtime: the moved file's
	// sub-unit rows are dropped with the old id below (DeleteByPath), and
	// only a re-parse rebuilds them under the new id. The incremental
	// reindex walk skips files whose recorded mtime+size match disk, so
	// recording the real mtime here would leave the moved file without
	// sub-units until its content next changes. A zero mtime guarantees
	// the next pass re-observes it.
	if releaseErr := fileState.Release(index.ReleaseContext{
		Path:        newRelPath,
		WorkerID:    workerID,
		Success:     true,
		State:       index.FileStateLive,
		ContentHash: newHash,
		MtimeNs:     0,
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

	referring, listErr := edgeRepo.ListByTargetOrSubUnits(oldID)

	if listErr != nil {
		return nil, listErr
	}

	// Structural rows — the moved file's own `contains` edges to its
	// sub-units — never carry rewritable text; without this filter the
	// moved file would count itself as a referrer.
	contentReferring := referring[:0:0]

	for _, edge := range referring {
		if edge.Kind != "structural" {
			contentReferring = append(contentReferring, edge)
		}
	}

	affectedFiles := uniqueSourcePaths(contentReferring)

	// A self-referencing note records oldPath as its edge's source_path,
	// but the file already lives at the new path by this point.
	currentPath := func(sourceFile string) string {
		if sourceFile == oldPath {
			return newRelPath
		}

		return sourceFile
	}

	for _, sourceFile := range affectedFiles {
		absSource := filepath.Join(root, currentPath(sourceFile))

		if rewriteErr := rewriteEdgeReferences(absSource, oldID, newID, edgeTypes); rewriteErr != nil {
			return nil, rewriteErr
		}
	}

	// Capture the outgoing edges BEFORE the node rows go away: DeleteByPath
	// cascades every edge sourced from the file (and from its sub-units),
	// so listing afterwards would find nothing to re-base.
	outgoing, listOutErr := edgeRepo.ListBySource(oldID)

	if listOutErr != nil {
		return nil, listOutErr
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

	rebased := make([]index.EdgeRow, 0, len(outgoing))

	for _, edge := range outgoing {
		// Structural contains edges stay behind: their sub-unit target
		// rows were dropped with the old path and only the next re-parse
		// recreates them (the destination file_state is released with a
		// stale mtime for exactly that reason) — a re-based contains edge
		// would dangle at a not-yet-existing sub-unit id until then.
		if edge.Kind == "structural" {
			continue
		}

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

	// Retarget every remaining incoming edge in one statement — including
	// rows sourced from other files' sub-units and rows targeting the moved
	// file's sub-unit ids. The per-file re-derive below replaces file-level
	// rows only, so without this the sub-unit-sourced rows would keep the
	// old target until their file's next re-parse.
	if retargetErr := edgeRepo.RetargetEdges(oldID, newID); retargetErr != nil {
		return nil, retargetErr
	}

	// Re-derive each affected file's file-level edges from its rewritten
	// content — frontmatter refs and body wikilinks alike — so the index
	// reflects the new target id.
	for _, sourceFile := range affectedFiles {
		relPath := currentPath(sourceFile)
		content, readErr := os.ReadFile(filepath.Join(root, relPath))

		if readErr != nil {
			return nil, fmt.Errorf("node: re-read %s: %w", relPath, readErr)
		}

		// ParseContentFile dispatches markdown vs HTML the same way the
		// reindex worker does — an HTML referrer has no frontmatter and
		// would abort the whole rename through plain ParseFile.
		parsed, parseErr := ParseContentFile(relPath, content)

		if parseErr != nil {
			return nil, parseErr
		}

		if resolveErr := ResolveEdges(parsed, edgeTypes); resolveErr != nil {
			return nil, resolveErr
		}

		// The reindex worker materializes body wikilinks and HTML links
		// into edges after resolving frontmatter refs; mirror it here or
		// the re-derive would erase every link-sourced file-level edge of
		// the referrer.
		MaterializeWikilinks(parsed, edgeTypes)
		MaterializeHTMLLinks(parsed, edgeTypes)

		if upsertErr := edgeRepo.UpsertAll(parsed.ID, parsed.Path, flattenEdges(parsed, nodeTypes)); upsertErr != nil {
			return nil, upsertErr
		}
	}

	// Report where each touched file lives now — for a self-referencing
	// note the recorded source_path is the old, no-longer-existing path.
	// Stays nil when no files were touched (the MCP envelope pins null).
	var reportedFiles []string

	for _, sourceFile := range affectedFiles {
		reportedFiles = append(reportedFiles, currentPath(sourceFile))
	}

	return &RenamePlan{
		OldID:         oldID,
		NewID:         newID,
		OldPath:       oldPath,
		NewPath:       newRelPath,
		AffectedFiles: reportedFiles,
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

// rewriteEdgeReferences reads the file at absPath, replaces every frontmatter
// scalar / sequence value matching oldID under any declared edge-type key with
// newID, rewrites body [[wikilinks]] targeting oldID, and writes the file back
// atomically.
func rewriteEdgeReferences(absPath, oldID, newID string, edgeTypes manifest.EdgeTypes) error {
	content, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return fmt.Errorf("node: read %s: %w", absPath, readErr)
	}

	rewritten := rewriteFrontmatterEdgeValues(content, oldID, newID, edgeTypes)
	rewritten = rewriteBodyWikilinks(rewritten, oldID, newID)

	// Skip the write when nothing changed (e.g. an HTML referrer whose
	// <a href> links are not rewritten) — a byte-identical rewrite would
	// still bump the mtime and wake the watcher for nothing.
	if bytes.Equal(rewritten, content) {
		return nil
	}

	if writeErr := os.WriteFile(absPath, rewritten, 0o644); writeErr != nil {
		return fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	return nil
}

// rewriteBodyWikilinks replaces `[[oldID]]` (and sub-unit deep links,
// `[[oldID#S1]]`) with the same link under newID in the markdown body — the
// region after the closing frontmatter delimiter. Fenced code blocks are
// rewritten too: file-level extraction skips fences, but the sub-unit
// pipeline derives edges from code-block content (fence markers are absent
// from unit text), so a `[[oldID]]` left inside a fence would flip the
// code-block and enclosing-section edges back to the dead id on the next
// re-parse.
func rewriteBodyWikilinks(content []byte, oldID, newID string) []byte {
	offset := bodyOffset(content)
	pattern := regexp.MustCompile(`\[\[\s*` + regexp.QuoteMeta(oldID) + `(#[^\[\]|]*)?\s*\]\]`)

	// ReplaceAllFunc rather than a $1 template: ids come from user paths,
	// so newID must be inserted verbatim, never expanded.
	replaceLink := func(match []byte) []byte {
		suffix := pattern.FindSubmatch(match)[1]

		return []byte("[[" + newID + string(suffix) + "]]")
	}

	var out bytes.Buffer

	out.Write(content[:offset])
	out.Write(pattern.ReplaceAllFunc(content[offset:], replaceLink))

	return out.Bytes()
}

// bodyOffset returns the byte offset where the markdown body begins: just
// past the closing frontmatter delimiter when the content carries a
// frontmatter block, else 0. Mirrors splitFrontmatter's delimiter handling
// without reassembling the content.
func bodyOffset(content []byte) int {
	trimmed := bytes.TrimLeft(content, " \t\r\n")
	lead := len(content) - len(trimmed)

	if !bytes.HasPrefix(trimmed, frontmatterDelimiter) {
		return 0
	}

	afterOpen := trimmed[len(frontmatterDelimiter):]
	closingIndex := bytes.Index(afterOpen, append([]byte("\n"), frontmatterDelimiter...))

	if closingIndex < 0 {
		return 0
	}

	return lead + len(frontmatterDelimiter) + closingIndex + len("\n") + len(frontmatterDelimiter)
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

		if retargeted, matched := retargetEdgeValue(trimmedValue, oldID, newID); matched {
			lines[lineIdx] = line[:colonIdx+1] + " " + yamlQuoteString(retargeted)

			continue
		}

		if strings.HasPrefix(trimmedValue, "[") && strings.HasSuffix(trimmedValue, "]") {
			inner := trimmedValue[1 : len(trimmedValue)-1]
			parts := strings.Split(inner, ",")

			for partIdx, part := range parts {
				if retargeted, matched := retargetEdgeValue(strings.TrimSpace(part), oldID, newID); matched {
					parts[partIdx] = " " + retargeted
				}
			}

			lines[lineIdx] = line[:colonIdx+1] + " [" + strings.Join(parts, ",") + "]"
		}
	}

	return []byte(strings.Join(lines, "\n"))
}

// retargetEdgeValue returns the rewritten frontmatter value when value — plain
// or quoted — refers to oldID or to a sub-unit id under it ("<oldID>#..."),
// preserving the sub-unit suffix. The second return reports whether the value
// matched.
func retargetEdgeValue(value, oldID, newID string) (string, bool) {
	unquoted := yamlUnquoteString(value)

	if unquoted == oldID {
		return newID, true
	}

	if strings.HasPrefix(unquoted, oldID+"#") {
		return newID + unquoted[len(oldID):], true
	}

	return "", false
}

// isSequenceItem reports whether line is a YAML block-sequence item ("- value"),
// ignoring leading indentation.
func isSequenceItem(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")

	return trimmed == "-" || strings.HasPrefix(trimmed, "- ")
}

// rewriteSequenceItem replaces a block-sequence item's value when it refers to
// oldID (or a sub-unit id under it), preserving the line's indentation and dash.
func rewriteSequenceItem(line, oldID, newID string) string {
	dashIdx := strings.IndexByte(line, '-')

	if dashIdx < 0 {
		return line
	}

	itemValue := strings.TrimSpace(line[dashIdx+1:])
	retargeted, matched := retargetEdgeValue(itemValue, oldID, newID)

	if !matched {
		return line
	}

	return line[:dashIdx+1] + " " + yamlQuoteString(retargeted)
}

// yamlUnquoteString reverses the quoting a frontmatter edge value can carry:
// yamlQuoteString's double-quoted scalars and yaml.v3's single-quoted scalars
// (renderMarkdown emits single quotes for a value that leads with a YAML
// indicator character, e.g. '@scope/foo'). Plain scalars are returned unchanged.
func yamlUnquoteString(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		// Single-quoted YAML: the sole escape is a doubled quote ('' -> ').
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}

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
