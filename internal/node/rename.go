package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"gopkg.in/yaml.v3"
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
	propertyDrift *index.PropertyDriftRepo,
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

	// The destination must stay in the source's node class. Rename derives the
	// new id by stripping the extension, so a cross-extension move (notes/x.md
	// → notes/b.txt) would mint an id that hijacks a sibling markdown node or,
	// for a non-indexable extension, an id the reindex walk never re-derives —
	// silently freezing the moved node out of the index (#686).
	if newExt, srcExt := filepath.Ext(newRelPath), filepath.Ext(row.Path); newExt != srcExt {
		return nil, fmt.Errorf("%w: source has %q, destination %q", ErrExtensionMismatch, srcExt, newExt)
	}

	if localErr := ensureVaultLocal(newRelPath); localErr != nil {
		return nil, localErr
	}

	if reservedErr := ensureIndexableID(newRelPath); reservedErr != nil {
		return nil, reservedErr
	}

	if ignoredErr := ensureNotIgnored(newRelPath); ignoredErr != nil {
		return nil, ignoredErr
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
	//
	// A case-only rename on a case-insensitive filesystem (APFS/NTFS) resolves
	// this stat to the SOURCE file itself — os.Stat("notes/Foo.md") returns
	// notes/foo.md's info — which the bare existence test misread as an
	// already-exists collision, making case-only renames impossible. os.Rename
	// performs the case change correctly, so treat "destination is the source"
	// as free rather than a collision (#686).
	if dstInfo, statErr := os.Stat(newAbs); statErr == nil {
		srcInfo, srcStatErr := os.Stat(oldAbs)

		if srcStatErr != nil || !os.SameFile(srcInfo, dstInfo) {
			return nil, abandonRenameLeases(fileState, loPath, hiPath, workerID,
				fmt.Errorf("node: rename target %s already exists", newRelPath))
		}
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

		if rewriteErr := rewriteEdgeReferences(absSource, oldID, newID, edgeTypes, nodeTypes); rewriteErr != nil {
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
	refLookup := NewIndexRefLookup(nodeRepo)

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

		// Resolve ref-typed properties (bare titles / wikilinks) to node ids,
		// exactly as every other derivation path does (worker.go, service.go,
		// edgewrite.go). Without this the re-derive wrote a referrer's raw
		// frontmatter value as the edge target, clobbering the resolved row
		// RetargetEdges had just written (#680). nodeTypes is nil only in unit
		// tests that declare no ref properties, so ResolveRefs is a no-op there.
		refResult := resolveRefEdges(parsed, nodeTypes, refLookup)

		// Record ref drift for this referrer the way the reindex worker does.
		// The re-derive drops a ref that no longer resolves; without a drift
		// row a pre-existing broken ref on a byte-unchanged referrer would go
		// invisible to `tusk doctor` and to the #679 heal pass. Recording it
		// also lets the heal pass converge the one transient case — a ref to a
		// sub-unit of the moved node, not re-indexed until the moved file's
		// next re-parse — on the next plain reindex.
		recordRefDrift(propertyDrift, parsed, refResult)

		// UpsertContentEdges, not UpsertAll: the referrer's kind='structural'
		// `contains` edges are supplied by the sub-unit pipeline, which does
		// not run on this path — a blanket delete would drop them until the
		// referrer's bytes next change (#680).
		if upsertErr := edgeRepo.UpsertContentEdges(parsed.ID, parsed.Path, flattenEdges(parsed, nodeTypes)); upsertErr != nil {
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

// recordRefDrift refreshes a re-derived referrer's ref-resolution drift rows
// from refResult, mirroring the reindex worker's clear-then-append: every
// ref-kind drift row for the node is cleared, then one row per current
// HardError is appended. A ref that now resolves drops its stale row; a
// still-broken ref stays visible to `tusk doctor` and the reindex heal pass
// (#679). driftRepo is nil on the read-only/test paths that pass no drift
// store, in which case recording is skipped.
func recordRefDrift(driftRepo *index.PropertyDriftRepo, parsed *Node, refResult RefResolutionResult) {
	if driftRepo == nil {
		return
	}

	_ = driftRepo.ClearRefKindsForNode(parsed.ID)

	observedAt := time.Now().UnixNano()

	for _, refErr := range refResult.HardErrors {
		details, _ := json.Marshal(map[string]any{
			"value":       refErr.Value,
			"to":          refErr.To,
			"candidates":  refErr.Candidates,
			"actual_type": refErr.ActualType,
		})

		_ = driftRepo.Append(index.PropertyDriftRow{
			NodeID:     parsed.ID,
			NodeType:   parsed.Type,
			Kind:       string(refErr.Kind),
			Property:   refErr.Property,
			Details:    string(details),
			ObservedAt: observedAt,
		})
	}
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
// atomically. nodeTypes lets the frontmatter rewriter tell a title-resolved ref
// property from an id-resolved edge type.
func rewriteEdgeReferences(absPath, oldID, newID string, edgeTypes manifest.EdgeTypes, nodeTypes map[string]manifest.NodeType) error {
	content, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return fmt.Errorf("node: read %s: %w", absPath, readErr)
	}

	rewritten := rewriteFrontmatterEdgeValues(content, oldID, newID, edgeTypes, nodeTypes)
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

// frontmatterEdit is one byte range of the frontmatter to replace with text.
type frontmatterEdit struct {
	start int
	end   int
	text  string
}

// rewriteFrontmatterEdgeValues replaces every frontmatter value under a declared
// edge-type key that names oldID — directly, as a sub-unit id beneath it
// ("<oldID>#S1"), or through a wikilink ("[[<oldID>]]") — with the same
// reference under newID.
//
// The frontmatter is parsed into a yaml.Node tree only to LOCATE those values:
// every scalar carries the line/column its token starts at, so the matching
// tokens are spliced out of the original bytes and everything else — key order,
// indentation, quoting style, comments, blank lines — survives untouched. The
// line-oriented rewriter this replaced could not see YAML structure, so it
// silently skipped every value that did not sit on a line of the exact shape it
// expected: a comment or blank line inside a block sequence, an inline trailing
// comment, a multi-line flow sequence, a wikilink-form ref (#680).
//
// Content with no frontmatter, or whose frontmatter does not parse, is returned
// unchanged. nodeTypes classifies each edge key as a ref property (bare value is
// a move-stable title, left alone) or an explicit edge type (bare value is a
// node id, rewritten); see retargetEdgeValue.
func rewriteFrontmatterEdgeValues(content []byte, oldID, newID string, edgeTypes manifest.EdgeTypes, nodeTypes map[string]manifest.NodeType) []byte {
	start, end, hasFrontmatter := frontmatterSpan(content)

	if !hasFrontmatter {
		return content
	}

	yamlText := content[start:end]

	var root yaml.Node

	if unmarshalErr := yaml.Unmarshal(yamlText, &root); unmarshalErr != nil {
		return content
	}

	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return content
	}

	var edits []frontmatterEdit

	mapping := root.Content[0].Content

	// A ref property's bare frontmatter value is a title (resolved by lookup),
	// not an id; the referrer's declared type decides which keys those are.
	refProps := refPropertyNamesForType(mappingScalarValue(mapping, "type"), nodeTypes)

	for pairIdx := 0; pairIdx+1 < len(mapping); pairIdx += 2 {
		key := mapping[pairIdx].Value

		if _, isEdge := edgeTypes[key]; !isEdge {
			continue
		}

		_, isRefProperty := refProps[key]
		bareIsID := !isRefProperty

		for _, scalar := range collectEdgeScalars(mapping[pairIdx+1], false) {
			retargeted, matched := retargetEdgeValue(scalar.node.Value, oldID, newID, bareIsID)

			if !matched {
				continue
			}

			tokenStart, tokenEnd, located := scalarSpan(yamlText, scalar.node)

			if !located {
				continue
			}

			edits = append(edits, frontmatterEdit{
				start: start + tokenStart,
				end:   start + tokenEnd,
				text:  yamlQuoteEdgeValue(retargeted, scalar.inFlow),
			})
		}
	}

	if len(edits) == 0 {
		return content
	}

	// Splice back-to-front so each edit's offsets stay valid.
	sort.Slice(edits, func(left, right int) bool { return edits[left].start > edits[right].start })

	rewritten := content

	for _, edit := range edits {
		var buffer bytes.Buffer

		buffer.Write(rewritten[:edit.start])
		buffer.WriteString(edit.text)
		buffer.Write(rewritten[edit.end:])

		rewritten = buffer.Bytes()
	}

	return rewritten
}

// flowScalar is a frontmatter scalar plus whether it sits inside a flow
// sequence, where a plain value must not contain a flow indicator.
type flowScalar struct {
	node   *yaml.Node
	inFlow bool
}

// mappingScalarValue returns the scalar value stored under key in a YAML mapping
// node's flattened [key, value, ...] content, or "" when the key is absent or
// non-scalar. Used to read the referrer's declared `type`.
func mappingScalarValue(mapping []*yaml.Node, key string) string {
	for pairIdx := 0; pairIdx+1 < len(mapping); pairIdx += 2 {
		if mapping[pairIdx].Value == key && mapping[pairIdx+1].Kind == yaml.ScalarNode {
			return mapping[pairIdx+1].Value
		}
	}

	return ""
}

// collectEdgeScalars returns the scalar leaves of an edge key's value: the value
// itself when it is a scalar, or the (possibly nested) items of a sequence.
// Mappings and aliases are skipped — neither is an edge-target shape.
func collectEdgeScalars(value *yaml.Node, inFlow bool) []flowScalar {
	switch value.Kind {
	case yaml.ScalarNode:
		return []flowScalar{{node: value, inFlow: inFlow}}
	case yaml.SequenceNode:
		nested := inFlow || value.Style&yaml.FlowStyle != 0

		var scalars []flowScalar

		for _, item := range value.Content {
			scalars = append(scalars, collectEdgeScalars(item, nested)...)
		}

		return scalars
	default:
		return nil
	}
}

// retargetEdgeValue returns the rewritten value when value refers to oldID, to a
// sub-unit id beneath it ("<oldID>#..."), or to either through a wikilink. The
// wikilink form is matched with the pattern ResolveRefs itself accepts, so
// exactly the values the resolver can resolve are the values a move rewrites —
// an aliased "[[oldID|Label]]" resolves nowhere and is left alone.
//
// bareIsID says whether a bare (non-wikilink) value under this key names a node
// id. It is true for an explicit edge type (frontmatter value = raw id) and
// false for a ref property, whose bare value is a TITLE resolved by lookup. A
// title is invariant under a move, so a bare title that merely coincides with
// the old id ("title: foo" on foo.md) must be left alone — rewriting it to the
// new id would break a reference that still resolves correctly (#680 review). A
// wikilink value is always an id and is rewritten regardless. The second return
// reports whether the value matched.
func retargetEdgeValue(value, oldID, newID string, bareIsID bool) (string, bool) {
	if wikilink := refWikilinkPattern.FindStringSubmatch(value); len(wikilink) == 2 {
		retargeted, matched := retargetNodeID(wikilink[1], oldID, newID)

		if !matched {
			return "", false
		}

		return "[[" + retargeted + "]]", true
	}

	if !bareIsID {
		return "", false
	}

	return retargetNodeID(value, oldID, newID)
}

// retargetNodeID rewrites a bare node id — oldID itself or a sub-unit id under
// it — to the same address under newID, preserving any sub-unit suffix.
func retargetNodeID(value, oldID, newID string) (string, bool) {
	if value == oldID {
		return newID, true
	}

	if strings.HasPrefix(value, oldID+"#") {
		return newID + value[len(oldID):], true
	}

	return "", false
}

// yamlQuoteEdgeValue quotes a rewritten edge value for the context it sits in.
// yamlQuoteString covers block context; inside a flow sequence a plain scalar
// must additionally carry no flow indicator, which would end the item early.
func yamlQuoteEdgeValue(value string, inFlow bool) string {
	quoted := yamlQuoteString(value)

	if !inFlow || strings.HasPrefix(quoted, `"`) || !strings.ContainsAny(quoted, ",[]{}") {
		return quoted
	}

	return yamlDoubleQuote(value)
}

// frontmatterSpan returns the byte range of the YAML frontmatter within content
// — the same slice splitFrontmatter hands the YAML decoder — so a yaml.Node's
// line/column addresses can be resolved against the original bytes.
func frontmatterSpan(content []byte) (int, int, bool) {
	trimmed := bytes.TrimLeft(content, " \t\r\n")
	lead := len(content) - len(trimmed)

	if !bytes.HasPrefix(trimmed, frontmatterDelimiter) {
		return 0, 0, false
	}

	afterOpen := trimmed[len(frontmatterDelimiter):]
	beforeNewlines := len(afterOpen)
	afterOpen = bytes.TrimLeft(afterOpen, "\r\n")

	closingIndex := bytes.Index(afterOpen, append([]byte("\n"), frontmatterDelimiter...))

	if closingIndex < 0 {
		return 0, 0, false
	}

	start := lead + len(frontmatterDelimiter) + (beforeNewlines - len(afterOpen))

	return start, start + closingIndex, true
}

// scalarSpan returns the byte range the scalar's token occupies in yamlText.
// A scalar's recorded position is where its token STARTS; the end depends on how
// it was written, so each style is measured on its own terms. Literal and folded
// blocks are rejected: a node id is never written as one, and their token has no
// single-line extent to splice.
func scalarSpan(yamlText []byte, scalar *yaml.Node) (int, int, bool) {
	start, located := byteOffsetAt(yamlText, scalar.Line, scalar.Column)

	if !located {
		return 0, 0, false
	}

	switch scalar.Style & (yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle) {
	case yaml.DoubleQuotedStyle:
		return quotedScalarSpan(yamlText, start, '"', true)
	case yaml.SingleQuotedStyle:
		return quotedScalarSpan(yamlText, start, '\'', false)
	case 0:
		// A plain scalar's source text is its value verbatim — unless YAML
		// folded it across lines, which the equality check rejects.
		end := start + len(scalar.Value)

		if end > len(yamlText) || string(yamlText[start:end]) != scalar.Value {
			return 0, 0, false
		}

		return start, end, true
	default:
		return 0, 0, false
	}
}

// quotedScalarSpan returns the byte range of the quoted scalar opening at start,
// including both quote characters. Double-quoted scalars escape with a
// backslash; single-quoted scalars escape a quote by doubling it.
func quotedScalarSpan(yamlText []byte, start int, quote byte, backslashEscapes bool) (int, int, bool) {
	if start >= len(yamlText) || yamlText[start] != quote {
		return 0, 0, false
	}

	for index := start + 1; index < len(yamlText); index++ {
		char := yamlText[index]

		if backslashEscapes && char == '\\' {
			index++

			continue
		}

		if char != quote {
			continue
		}

		if !backslashEscapes && index+1 < len(yamlText) && yamlText[index+1] == quote {
			index++

			continue
		}

		return start, index + 1, true
	}

	return 0, 0, false
}

// byteOffsetAt converts a yaml.Node's 1-based line and column into a byte offset
// into yamlText. Columns count runes, not bytes, so a line carrying multi-byte
// characters before the token must be walked rune by rune.
func byteOffsetAt(yamlText []byte, line, column int) (int, bool) {
	if line < 1 || column < 1 {
		return 0, false
	}

	offset := 0

	for current := 1; current < line; current++ {
		lineEnd := bytes.IndexByte(yamlText[offset:], '\n')

		if lineEnd < 0 {
			return 0, false
		}

		offset += lineEnd + 1
	}

	for remaining := column - 1; remaining > 0; remaining-- {
		if offset >= len(yamlText) || yamlText[offset] == '\n' {
			return 0, false
		}

		_, size := utf8.DecodeRune(yamlText[offset:])
		offset += size
	}

	return offset, true
}
