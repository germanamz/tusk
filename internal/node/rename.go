package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/wikilink"
	"gopkg.in/yaml.v3"
)

// Delete removes the file for the given node id, deletes the node row and
// outgoing edges from the index, and enqueues every file whose derived edge
// pointed at the deleted node so a later reindex re-resolves it (#689). Incoming
// direct edges are left as dangling references (surfaced by tusk doctor).
//
// The file removal goes through WriteWithLease so concurrent writers
// serialize on file_state and the row transitions to state='tombstone'
// as a soft-delete audit record.
//
// queue may be nil (referrer wake-up is then skipped): the reindex reap wakes
// the same referrers, so callers without a queue simply defer convergence to
// the next reindex. linkCase is how name-form wikilinks match file names when
// the delete re-resolves them.
func Delete(
	root string,
	nodeRepo *index.NodeRepo,
	edgeRepo *index.EdgeRepo,
	fileState *index.FileStateRepo,
	queue *index.EmbedQueueRepo,
	workerID string,
	leaseTTL time.Duration,
	linkCase wikilink.Case,
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

	// Capture the referrers whose derived edge pointed here BEFORE dropping the
	// node row (DeleteBySource only removes rows sourced FROM nodeID; the
	// incoming rows survive), then wake them so the next drain re-resolves.
	if enqErr := EnqueueDerivedReferrers(edgeRepo, queue, nodeID); enqErr != nil {
		return enqErr
	}

	if deleteEdgesErr := edgeRepo.DeleteBySource(nodeID); deleteEdgesErr != nil {
		return deleteEdgesErr
	}

	if deletePathErr := nodeRepo.DeleteByPath(row.Path); deletePathErr != nil {
		return deletePathErr
	}

	// The deleted file no longer answers to its name: a [[name]] that
	// resolved to it dangles, and one that was ambiguous between it and
	// another file now resolves to the other (#791).
	return index.ResolveLinks(nodeRepo, edgeRepo, linkCase)
}

// EnqueueDerivedReferrers re-enqueues every file whose DERIVED edge resolved to
// targetID so a later drain re-derives its refs. Call it when targetID's node is
// removed (reindex reap, `node delete`): the referrer is otherwise byte-unchanged
// and walk-skipped, and no drift row was recorded, so nothing wakes it — its
// derived edge stays frozen at the dead id until a `reindex --force` (#689).
// Re-resolution retargets the edge when the ref's title now resolves to a
// renamed file, or drops it and records ref drift when the target is gone (or
// reappeared as a different type).
//
// Only derived edges are woken: direct and structural edges are not ref-property
// resolved, and their dangling is surfaced by doctor as designed. A nil queue or
// edges repo is a no-op.
func EnqueueDerivedReferrers(edgeRepo *index.EdgeRepo, queue *index.EmbedQueueRepo, targetID string) error {
	if edgeRepo == nil || queue == nil {
		return nil
	}

	incoming, listErr := edgeRepo.ListByTarget(targetID)

	if listErr != nil {
		return fmt.Errorf("node: list referrers of %s: %w", targetID, listErr)
	}

	enqueued := map[string]bool{}

	for _, edge := range incoming {
		if edge.Kind != "derived" || enqueued[edge.SourcePath] {
			continue
		}

		enqueued[edge.SourcePath] = true

		if enqErr := queue.EnqueueReindex(edge.SourcePath); enqErr != nil {
			return fmt.Errorf("node: enqueue referrer %s: %w", edge.SourcePath, enqErr)
		}
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
	lineNumbering linenum.Scheme,
	linkCase wikilink.Case,
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

	// Derive the new id the way ParseContentFile will when the moved file is
	// next re-parsed: HTML keeps its extension, markdown strips it. Stripping
	// unconditionally minted a phantom row (id "docs/b", path "docs/b.html")
	// that corrupted referrer links and wedged reindex on a UNIQUE nodes.path
	// constraint forever (#687).
	newID := nodeIDForPath(newRelPath)
	oldPath := row.Path

	// The moved file's indexed type, read before its rows move: the parse
	// fallback for a file adopted under the workspace default type (#790).
	movedType := fileNodeType(nodeRepo, row)
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

	// The file names as they stand before the move, read while the node rows
	// still have the old id: every link is judged by what it named here.
	namesBefore, namesErr := nodeRepo.LinkNames(linkCase)

	if namesErr != nil {
		return nil, namesErr
	}

	move := newLinkMove(namesBefore, oldID, newID, newRelPath)

	// Another note whose name the moved file now shares loses its [[name]]
	// links to ambiguity, or to the moved file outright when it lands at the
	// root under that name. Its referrers are rewritten too (to its full id),
	// so a move never breaks or redirects a link (#791).
	collisionFiles, collisionErr := collisionReferrers(edgeRepo, namesBefore, oldID, wikilink.FileName(newRelPath))

	if collisionErr != nil {
		return nil, collisionErr
	}

	// The name-form links each rewritten file now reads differently, keyed by
	// where the file lives after the move.
	renamedLinks := map[string][]linkRename{}

	for _, sourceFile := range affectedFiles {
		relSource := currentPath(sourceFile)
		absSource := filepath.Join(root, relSource)

		rewrite, rewriteErr := rewriteEdgeReferences(absSource, relSource, sourceFile, move, edgeTypes, nodeTypes)

		if rewriteErr != nil {
			return nil, rewriteErr
		}

		renamedLinks[relSource] = rewrite.renames
	}

	// The moved file's own relative links were written from its old folder:
	// re-render them from the new one so they keep naming the same files. A
	// self-referencing note was rewritten above, with the same base.
	if !slices.Contains(affectedFiles, oldPath) && !IsHTMLPath(newRelPath) {
		rewrite, rewriteErr := rewriteEdgeReferences(newAbs, newRelPath, oldPath, move, edgeTypes, nodeTypes)

		if rewriteErr != nil {
			return nil, rewriteErr
		}

		renamedLinks[newRelPath] = rewrite.renames
	}

	for _, sourceFile := range collisionFiles {
		if slices.Contains(affectedFiles, sourceFile) || sourceFile == oldPath {
			continue
		}

		rewrite, rewriteErr := rewriteEdgeReferences(filepath.Join(root, sourceFile), sourceFile, sourceFile, move, edgeTypes, nodeTypes)

		if rewriteErr != nil {
			return nil, rewriteErr
		}

		if rewrite.changed {
			affectedFiles = append(affectedFiles, sourceFile)
			renamedLinks[sourceFile] = rewrite.renames
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
			Link:       edge.Link,
		})
	}

	if upsertErr := edgeRepo.UpsertAll(newID, newRelPath, rebased); upsertErr != nil {
		return nil, upsertErr
	}

	// The moved page's path refs cascaded with its old row. Re-derive them
	// rather than carry them over: a relative link resolves against the
	// page's directory, so the same bytes can name other paths from here.
	movedContent, readMovedErr := os.ReadFile(newAbs)

	if readMovedErr != nil {
		return nil, fmt.Errorf("node: re-read %s: %w", newRelPath, readMovedErr)
	}

	if syncErr := syncPathRefs(edgeRepo, newRelPath, movedContent, movedType, edgeTypes, lineNumbering); syncErr != nil {
		return nil, syncErr
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
	refLookup := NewIndexRefLookup(nodeRepo, linkCase)

	for _, sourceFile := range affectedFiles {
		relPath := currentPath(sourceFile)
		content, readErr := os.ReadFile(filepath.Join(root, relPath))

		if readErr != nil {
			return nil, fmt.Errorf("node: re-read %s: %w", relPath, readErr)
		}

		// A referrer adopted under the workspace default type (#790) has no
		// type of its own; its indexed type stands in for it.
		referrerType := indexedType(nodeRepo, relPath)

		// The rewrite retargeted what the referrer's links name (an HTML
		// href now points at the new path), so its path refs follow.
		if syncErr := syncPathRefs(edgeRepo, relPath, content, referrerType, edgeTypes, lineNumbering); syncErr != nil {
			return nil, syncErr
		}

		// ParseContentFile dispatches markdown vs HTML the same way the
		// reindex worker does — an HTML referrer has no frontmatter and
		// would abort the whole rename through plain ParseFile.
		parsed, parseErr := ParseContentFileAs(relPath, content, referrerType)

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
		if upsertErr := edgeRepo.UpsertContentEdges(parsed.ID, parsed.Path, EdgeRows(parsed, nodeTypes)); upsertErr != nil {
			return nil, upsertErr
		}
	}

	// The re-derive rebuilt only file-level rows. A renamed link is also
	// carried by the rows of the sub-units around it, and by the moved file's
	// re-based rows, which still hold the old text.
	for sourcePath, renames := range renamedLinks {
		for _, rename := range renames {
			if renameErr := edgeRepo.RenameLink(sourcePath, rename.oldName, rename.newName); renameErr != nil {
				return nil, renameErr
			}
		}
	}

	// Every name-form link in the vault resolves against the moved file's new
	// name now, including the ones the re-derive above stored as written.
	if resolveErr := index.ResolveLinks(nodeRepo, edgeRepo, linkCase); resolveErr != nil {
		return nil, resolveErr
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
			Value:      refErr.Value,
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

// collisionReferrers returns the files that link to a note other than oldID
// whose file name matches newName before the move: the notes whose [[name]]
// links the moved file can take over by arriving under that name.
func collisionReferrers(edgeRepo *index.EdgeRepo, before *wikilink.Names, oldID, newName string) ([]string, error) {
	named, candidates, _ := before.ResolveName(newName)

	if named != "" {
		candidates = append(candidates, named)
	}

	var files []string

	for _, candidate := range candidates {
		if candidate == oldID {
			continue
		}

		referring, listErr := edgeRepo.ListByTargetOrSubUnits(candidate)

		if listErr != nil {
			return nil, listErr
		}

		for _, sourceFile := range uniqueSourcePaths(referring) {
			if !slices.Contains(files, sourceFile) {
				files = append(files, sourceFile)
			}
		}
	}

	return files, nil
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

// rewriteEdgeReferences reads the referrer at absPath (whose workspace-relative
// path is relPath) and rewrites every reference the move changes, then writes
// the file back atomically. It reports whether the file changed and which
// name-form links it renamed. A markdown referrer's references live in
// frontmatter edge values and body [[wikilinks]], judged by move as they read
// from base: the referrer's own path, except for the moved file itself, whose
// links were written from its old folder. An HTML referrer's live in <a href>
// values, which resolve to node ids the same way the edge derivation does, so
// the two content kinds take disjoint rewriters. nodeTypes lets the frontmatter
// rewriter tell a title-resolved ref property from an id-resolved edge type.
func rewriteEdgeReferences(absPath, relPath, base string, move *linkMove, edgeTypes manifest.EdgeTypes, nodeTypes map[string]manifest.NodeType) (linkRewrite, error) {
	content, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return linkRewrite{}, fmt.Errorf("node: read %s: %w", absPath, readErr)
	}

	var (
		rewritten []byte
		renames   []linkRename
	)

	if IsHTMLPath(relPath) {
		// HTML referrers carry no YAML frontmatter and no [[wikilinks]] — their
		// only references are <a href> values. Rewriting the href on disk keeps
		// the reference following the move, the HTML counterpart of the markdown
		// wikilink rewrite below; without it the re-derive loop re-parses the
		// stale href and reverts the retargeted edge to the dead id (#687).
		rewritten = rewriteHTMLHrefs(content, relPath, move.oldID, move.newID)
	} else {
		retarget := move.retargetFrom(base, relPath)

		// Record every name the rewrite changes: the rows of the file's
		// sub-units carry the old text, and only the file's own rows are
		// re-derived from disk.
		recording := func(pathPart string) (string, bool) {
			newPath, changed := retarget(pathPart)

			if changed && wikilink.IsName(pathPart) && !slices.Contains(renames, linkRename{pathPart, newPath}) {
				renames = append(renames, linkRename{oldName: pathPart, newName: newPath})
			}

			return newPath, changed
		}

		rewritten = rewriteFrontmatterEdgeValues(content, move.oldID, move.newID, recording, edgeTypes, nodeTypes)
		rewritten = rewriteBodyLinks(rewritten, recording)
	}

	// Skip the write when nothing changed (e.g. an HTML referrer whose only
	// <a href> points elsewhere) — a byte-identical rewrite would still bump
	// the mtime and wake the watcher for nothing.
	if bytes.Equal(rewritten, content) {
		return linkRewrite{}, nil
	}

	if writeErr := os.WriteFile(absPath, rewritten, 0o644); writeErr != nil {
		return linkRewrite{}, fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	return linkRewrite{changed: true, renames: renames}, nil
}

// linkRewrite is what rewriteEdgeReferences did to one file: whether it wrote
// it, and which name-form links it changed.
type linkRewrite struct {
	changed bool
	renames []linkRename
}

// linkRename is one name-form link a move rewrote: [[oldName]] now reads
// [[newName]], a name or a full id.
type linkRename struct {
	oldName string
	newName string
}

// linkRetarget maps the path part of a link (its target without a #fragment or
// |alias) to what the link should say after a move. false leaves the link as
// written.
type linkRetarget func(pathPart string) (string, bool)

// retargetLinkTarget applies retarget to a whole link target, keeping its
// #fragment.
func retargetLinkTarget(target string, retarget linkRetarget) (string, bool) {
	pathPart, fragment := wikilink.SplitFragment(strings.TrimSpace(target))
	newPath, changed := retarget(pathPart)

	if !changed {
		return "", false
	}

	return newPath + fragment, true
}

// linkMove decides how a move changes each link in the vault. It holds the
// file names before the move and after it, so a link is judged by whether it
// still names the file it named: the rule that keeps a move from breaking or
// redirecting any link (#791).
//
//   - A workspace path naming the moved file takes its new id.
//   - A relative path is re-rendered from where its file lives now, naming the
//     moved file's new id, or the same file as before.
//   - A name keeps its text while it still names the same file. When it no
//     longer does (the moved file was renamed, or another file now shares the
//     name), it becomes that file's name if the name alone still finds it, and
//     its full id otherwise. That covers the files whose links pointed at
//     another note the moved file now shares a name with.
type linkMove struct {
	oldID  string
	newID  string
	before *wikilink.Names
	after  *wikilink.Names
}

// newLinkMove builds the linkMove for a move of oldID to newID at newPath, from
// the file names as they stand before it.
func newLinkMove(before *wikilink.Names, oldID, newID, newPath string) *linkMove {
	after := before.Clone()
	after.Remove(oldID)
	after.Add(newID, newPath)

	return &linkMove{oldID: oldID, newID: newID, before: before, after: after}
}

// retargetFrom returns the linkRetarget for the links of one file: they resolve
// from base, and the file lives at writer once the move is done.
func (move *linkMove) retargetFrom(base, writer string) linkRetarget {
	return func(pathPart string) (string, bool) {
		switch {
		case wikilink.IsRelative(pathPart):
			return move.retargetRelative(pathPart, base, writer)
		case wikilink.IsName(pathPart):
			return move.retargetName(pathPart)
		case pathPart == move.oldID:
			return move.newID, true
		default:
			return "", false
		}
	}
}

// retargetRelative re-renders a relative link from writer.
func (move *linkMove) retargetRelative(pathPart, base, writer string) (string, bool) {
	id := wikilink.Normalize(pathPart, base)

	// Normalize keeps a path that climbs out of the workspace as written.
	if id == pathPart {
		return "", false
	}

	if id == move.oldID {
		id = move.newID
	} else if base == writer {
		return "", false
	}

	rendered := wikilink.RelativeTo(writer, id)

	return rendered, rendered != pathPart
}

// retargetName keeps a name while it still names the same file.
func (move *linkMove) retargetName(name string) (string, bool) {
	named, _, _ := move.before.ResolveName(name)

	if named == "" {
		return "", false
	}

	if named == move.oldID {
		named = move.newID
	}

	if now, _, _ := move.after.ResolveName(name); now == named {
		return "", false
	}

	targetPath, _ := move.after.Path(named)
	shortest := wikilink.FileName(targetPath)

	if found, _, _ := move.after.ResolveName(shortest); found == named {
		return shortest, true
	}

	return named, true
}

// htmlAnchorTagPattern matches an anchor open tag; htmlHrefAttrPattern matches
// a space-delimited href attribute (with its leading whitespace, so it never
// straddles a data-href / other-href attribute) and captures the assignment and
// the value token separately. Both are case-insensitive and dot-matches-newline
// so a multi-line <a> tag is still one match.
var (
	htmlAnchorTagPattern = regexp.MustCompile(`(?is)<a\b[^>]*>`)
	htmlHrefAttrPattern  = regexp.MustCompile(`(?is)(\shref\s*=\s*)("[^"]*"|'[^']*'|[^\s"'>]+)`)
)

// rewriteHTMLHrefs rewrites every <a href> value that resolves to oldID so it
// resolves to newID instead, editing the raw bytes in place. Only href
// attributes on anchor elements are touched — the same set collectHrefs turns
// into edges — so a coincidental href on another element is left alone. Quote
// style, attribute whitespace, and any ?query / #fragment are preserved; a
// value that does not resolve to oldID is untouched.
func rewriteHTMLHrefs(content []byte, sourcePath, oldID, newID string) []byte {
	return htmlAnchorTagPattern.ReplaceAllFunc(content, func(tag []byte) []byte {
		return htmlHrefAttrPattern.ReplaceAllFunc(tag, func(attr []byte) []byte {
			groups := htmlHrefAttrPattern.FindSubmatch(attr)
			assign, valueToken := groups[1], groups[2]

			rawValue, quote := unquoteAttrValue(valueToken)

			retargeted, changed := retargetHref(rawValue, sourcePath, oldID, newID)

			if !changed {
				return attr
			}

			return []byte(string(assign) + requoteAttrValue(retargeted, quote))
		})
	})
}

// retargetHref rewrites a single href value that resolves to oldID into the
// value resolving to newID, preserving any ?query / #fragment verbatim and the
// dir-relative vs root-relative style. The second return reports whether the
// value matched (anything ResolveHTMLLinks drops — external, in-page anchor,
// vault-escape — never matches and is left alone).
func retargetHref(rawValue, sourcePath, oldID, newID string) (string, bool) {
	// Split off ?query / #fragment so only the path portion is retargeted and
	// the remainder survives byte-for-byte (no URL re-encoding round-trip).
	pathPart := rawValue
	suffix := ""

	if cut := strings.IndexAny(rawValue, "?#"); cut >= 0 {
		pathPart, suffix = rawValue[:cut], rawValue[cut:]
	}

	// Reuse the exact resolver the edge derivation uses: an href names oldID
	// iff ResolveHTMLLinks returns it for this source.
	resolved := ResolveHTMLLinks(sourcePath, []string{pathPart})

	if len(resolved) != 1 || resolved[0] != oldID {
		return "", false
	}

	return relativeHref(sourcePath, newID, strings.HasPrefix(strings.TrimSpace(pathPart), "/")) + suffix, true
}

// relativeHref renders target id as an <a href> path from sourcePath's
// directory. A root-relative original href ("/notes/x") stays root-relative;
// otherwise the result is a "../"-style path relative to the source file's
// directory. Forward slashes throughout — these are URL paths, not OS paths, so
// path (not filepath) computes the directory.
func relativeHref(sourcePath, targetID string, rootRelative bool) string {
	if rootRelative {
		return "/" + targetID
	}

	fromDir := path.Dir(sourcePath)

	if fromDir == "." {
		return targetID
	}

	fromSegments := strings.Split(fromDir, "/")
	targetSegments := strings.Split(targetID, "/")

	common := 0

	for common < len(fromSegments) && common < len(targetSegments) && fromSegments[common] == targetSegments[common] {
		common++
	}

	var segments []string

	for depth := common; depth < len(fromSegments); depth++ {
		segments = append(segments, "..")
	}

	segments = append(segments, targetSegments[common:]...)

	return strings.Join(segments, "/")
}

// unquoteAttrValue splits an attribute value token into its inner value and the
// quote byte that wrapped it (0 for an unquoted value).
func unquoteAttrValue(token []byte) (string, byte) {
	if len(token) >= 2 && (token[0] == '"' || token[0] == '\'') && token[len(token)-1] == token[0] {
		return string(token[1 : len(token)-1]), token[0]
	}

	return string(token), 0
}

// requoteAttrValue re-wraps value in the quote style it was read with.
func requoteAttrValue(value string, quote byte) string {
	if quote == 0 {
		return value
	}

	return string(quote) + value + string(quote)
}

// bodyLinkPattern matches a wikilink the way wikilink.Extract does: the
// captured group is everything between `[[` and `]]`.
var bodyLinkPattern = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// rewriteBodyLinks rewrites every [[wikilink]] in the markdown body — the
// region after the closing frontmatter delimiter — whose target retarget
// changes. Only the target's path is retargeted: a sub-unit `#fragment` and an
// Obsidian `|Label` display suffix stay verbatim, so a move never disturbs a
// deep-link address or the display text (#690), while the spacing inside the
// brackets normalizes (`[[ old ]]` becomes `[[new]]`). Each link is judged as a
// whole target, so a longer id that merely starts with the moved one is never
// touched. Code is left as written: a link inside an inline code span or a code
// block is an example of the syntax, and no level of the index derives an edge
// from it (#788). Links are found in wikilink.MaskCode's mask, the view
// wikilink.Extract scans, and replaced in the original bytes.
func rewriteBodyLinks(content []byte, retarget linkRetarget) []byte {
	offset := bodyOffset(content)
	body := content[offset:]

	var out bytes.Buffer

	out.Write(content[:offset])

	last := 0

	// Built by hand rather than from a $1 template: ids come from user paths,
	// so the new target must be inserted verbatim, never expanded.
	for _, match := range bodyLinkPattern.FindAllSubmatchIndex(wikilink.MaskCode(body), -1) {
		inner := string(body[match[2]:match[3]])
		cut := strings.IndexAny(inner, "#|")

		if cut < 0 {
			cut = len(inner)
		}

		newPath, changed := retarget(strings.TrimSpace(inner[:cut]))

		if !changed {
			continue
		}

		out.Write(body[last:match[0]])
		out.WriteString("[[" + newPath + inner[cut:] + "]]")

		last = match[1]
	}

	out.Write(body[last:])

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
// edge-type key that names oldID, directly or as a sub-unit id beneath it
// ("<oldID>#S1"), with the same reference under newID, and every value written
// as a wikilink that retarget changes (a path, relative path or name the move
// affects, as for a body link; #791).
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
func rewriteFrontmatterEdgeValues(content []byte, oldID, newID string, retarget linkRetarget, edgeTypes manifest.EdgeTypes, nodeTypes map[string]manifest.NodeType) []byte {
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
	referrerType := mappingScalarValue(mapping, "type")
	refProps := refPropertyNamesForType(referrerType, nodeTypes)

	for pairIdx := 0; pairIdx+1 < len(mapping); pairIdx += 2 {
		key := mapping[pairIdx].Value
		edgeType, isEdge := edgeTypes[key]

		if !isEdge {
			continue
		}

		// A paths-only type's values name workspace paths, not nodes (see
		// ResolveEdges); like a link or a code span, a move leaves them as
		// written.
		if edgeType.PathsOnly() && edgeType.AllowsSource(referrerType) {
			continue
		}

		value := mapping[pairIdx+1]

		// An unquoted frontmatter wikilink `key: [[id]]` decodes to two nested
		// flow sequences whose brackets are the wikilink's own (#692). Retarget
		// only the inner scalar so the `[[` `]]` stay put and the value is never
		// re-quoted. The scalar is always an id (a wikilink is), so bareIsID does
		// not gate it — an unquoted ref wikilink retargets like an edge one.
		if scalar, isWikilink := nestedWikilinkScalar(value); isWikilink {
			target, alias := wikilink.SplitAlias(scalar.Value)

			retargeted, matched := retargetLinkTarget(target, retarget)

			if !matched {
				continue
			}

			tokenStart, tokenEnd, located := scalarSpan(yamlText, scalar)

			if !located {
				continue
			}

			edits = append(edits, frontmatterEdit{
				start: start + tokenStart,
				end:   start + tokenEnd,
				text:  retargeted + alias,
			})

			continue
		}

		_, isRefProperty := refProps[key]
		bareIsID := !isRefProperty

		for _, scalar := range collectEdgeScalars(value, false) {
			retargeted, matched := retargetEdgeValue(scalar.node.Value, oldID, newID, retarget, bareIsID)

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

// nestedWikilinkScalar returns the lone scalar of a value shaped as an unquoted
// `[[id]]` wikilink — a single-item flow sequence whose single item is a
// single-item flow sequence holding one scalar — and false for any other shape.
// The outer and inner `[` `]` are the wikilink's own brackets, so the scalar
// carries the target id plus any `|alias` suffix; rewriting just that scalar
// leaves the brackets (and thus the wikilink) intact (#692).
func nestedWikilinkScalar(value *yaml.Node) (*yaml.Node, bool) {
	if value.Kind != yaml.SequenceNode || len(value.Content) != 1 {
		return nil, false
	}

	inner := value.Content[0]

	if inner.Kind != yaml.SequenceNode || len(inner.Content) != 1 {
		return nil, false
	}

	scalar := inner.Content[0]

	if scalar.Kind != yaml.ScalarNode {
		return nil, false
	}

	return scalar, true
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

// retargetEdgeValue returns the rewritten value when value refers to oldID or to
// a sub-unit id beneath it ("<oldID>#..."), or is a wikilink retarget changes —
// including the Obsidian aliased form "[[target|Label]]". The wikilink is
// matched with the pattern ResolveRefs itself accepts, split on the alias the
// same way (#690), and judged by retarget, which resolves it as ResolveRefs
// does (#791), so exactly the values the resolver resolves are the values a
// move rewrites; the "|Label" display suffix is preserved and only the target
// ahead of it is retargeted.
//
// bareIsID says whether a bare (non-wikilink) value under this key names a node
// id. It is true for an explicit edge type (frontmatter value = raw id) and
// false for a ref property, whose bare value is a TITLE resolved by lookup. A
// title is invariant under a move, so a bare title that merely coincides with
// the old id ("title: foo" on foo.md) must be left alone — rewriting it to the
// new id would break a reference that still resolves correctly (#680 review). A
// wikilink value is judged by retarget whatever the key. The second return
// reports whether the value matched.
func retargetEdgeValue(value, oldID, newID string, retarget linkRetarget, bareIsID bool) (string, bool) {
	if match := refWikilinkPattern.FindStringSubmatch(value); len(match) == 2 {
		target, alias := wikilink.SplitAlias(match[1])

		retargeted, matched := retargetLinkTarget(target, retarget)

		if !matched {
			return "", false
		}

		return "[[" + retargeted + alias + "]]", true
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
// — the same slice splitFrontmatter hands the YAML decoder, past a leading BOM
// — so a yaml.Node's line/column addresses can be resolved against the
// original bytes.
func frontmatterSpan(content []byte) (int, int, bool) {
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(content, utf8BOM), " \t\r\n")
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
