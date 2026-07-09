package subunit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// Sync applies a parser-produced []Unit to the SQLite index for a single
// file. It diffs the new hash set against the existing sub-unit rows for
// the file, inserts the new ones, deletes the missing ones, updates the
// ordinal on units that moved, re-derives outbound wikilink edges for
// inserted and content-changed units, replaces the file's `contains`
// edges, and enqueues embedding work for inserted or content-changed
// leaves.
//
// All four repositories are required; the manifest is required so the
// sync can discover which edge types opt into wikilinks. Logger is
// optional; when nil, no log lines are emitted.
//
// Transactional shape: each repository call commits independently. A
// partial failure mid-file leaves the file in a mixed state — the next
// reindex pass re-runs the diff and converges. Wrapping the entire
// ApplyFile in a single transaction would require routing every repo
// method through a shared *sql.Tx; the repos do not currently take a
// transaction, and the spec's "single transaction" intent (§5.5) is
// honored by the repos individually (BulkUpsert, BulkDelete, and
// EdgeRepo.UpsertAll all use their own transactions). Future work may
// promote ApplyFile to a single tx once the repos grow tx-accepting
// siblings.
type Sync struct {
	// Repo persists sub-unit NodeRow values. Required.
	Repo *index.NodeRepo
	// EdgeRepo persists outbound edges from each sub-unit and the
	// `contains` edges from the file to its sub-units. Required.
	EdgeRepo *index.EdgeRepo
	// EmbedQ enqueues embedding work for inserted leaves. Required when
	// embedding is enabled; nil disables embed enqueue without
	// affecting node/edge writes.
	EmbedQ *index.EmbedQueueRepo
	// Manifest carries the workspace's edge-type declarations so the
	// sync can resolve which edge types opt into wikilinks. Required.
	Manifest *manifest.Manifest
	// Logger receives a structured `sync apply` line per file. Optional.
	Logger *slog.Logger
	// Source names the namespace stamped on this file's sub-unit rows
	// and structural edges (the nodes.source and edges.source columns).
	// The zero value ("") is treated as "markdown" for back-compat so
	// existing markdown callers need no change. The HTML pipeline sets
	// it to "html"; the value must match a built-in typepack Source().
	Source string
}

// SyncResult summarizes the work ApplyFile performed for one file. The
// reindex Report aggregates these across the walk.
type SyncResult struct {
	// Inserted is the count of sub-unit rows newly written.
	Inserted int
	// Deleted is the count of sub-unit rows removed.
	Deleted int
	// Reordered is the count of sub-unit rows whose ordinal changed
	// (hash unchanged).
	Reordered int
}

// containsEdgeType is the manifest name of the file→sub-unit edge
// declared by the built-in sub-document type pack. Centralized so the
// edge writer and tests reference the same string.
const containsEdgeType = "contains"

// structuralEdgeKind tags edges produced by the sub-unit pipeline so the
// schema's kind column reflects the writer path.
const structuralEdgeKind = "structural"

// defaultSyncSource names the namespace used when Sync.Source is the
// zero value. Keeps pre-existing markdown callers byte-identical: an
// unset Source still stamps "markdown" on edges.source and nodes.source.
const defaultSyncSource = "markdown"

// resolvedSource returns the effective namespace for this sync: the
// explicit Source when set, else defaultSyncSource.
func (sync *Sync) resolvedSource() string {
	if sync.Source == "" {
		return defaultSyncSource
	}

	return sync.Source
}

// ApplyFile diffs units against the existing sub-unit rows for fileRow
// and converges the index to match. The parent file row must already be
// upserted; ApplyFile only writes sub-unit rows under fileRow.ID. units
// may be empty — in that case ApplyFile deletes every sub-unit row for
// the file and clears its `contains` edges.
//
// The ctx is propagated to the repository calls that support it (none
// today; reserved for future tx-aware variants).
func (sync *Sync) ApplyFile(ctx context.Context, fileRow index.NodeRow, units []Unit) (*SyncResult, error) {
	if sync == nil || sync.Repo == nil || sync.EdgeRepo == nil || sync.Manifest == nil {
		return nil, fmt.Errorf("subunit sync: Repo, EdgeRepo, and Manifest are required")
	}

	result := &SyncResult{}

	// ListSubUnitsForFile returns every sub-unit row for the file,
	// not just the immediate children of fileRow.ID. Sub-units nested
	// inside section units carry a parent_id pointing at the section
	// (a sibling sub-unit row), not at the file. Diffing must consider
	// the whole tree so paragraphs under a deleted section are dropped
	// alongside it.
	existing, listErr := sync.Repo.ListSubUnitsForFile(fileRow.ID)

	if listErr != nil {
		return nil, fmt.Errorf("subunit sync: list existing %s: %w", fileRow.ID, listErr)
	}

	// Index existing rows by their structural address (the suffix of the
	// row id after the "#") so we can diff against parser addresses.
	existingByAddr := make(map[string]index.NodeRow, len(existing))

	for _, row := range existing {
		addr := addressFromID(row.ID, fileRow.ID)

		if addr == "" {
			continue
		}

		existingByAddr[addr] = row
	}

	// Bucket the new units the same way for a symmetric diff.
	newByAddr := make(map[string]Unit, len(units))

	for _, unit := range units {
		newByAddr[unitAddress(unit)] = unit
	}

	// Inserted: addresses present in new \ existing. The order is the
	// parser's depth-first emission order, so downstream callers receive
	// deterministic enqueue and edge-write order.
	var insertRows []index.NodeRow
	var insertedUnits []Unit

	// Reordered/changed: addresses in both whose ordinal or content moved.
	var reorderRows []index.NodeRow

	for _, unit := range units {
		addr := unitAddress(unit)
		subunitID := subunitRowID(fileRow.ID, addr)

		row, buildErr := buildSubUnitRow(fileRow, unit, subunitID)

		if buildErr != nil {
			return nil, buildErr
		}

		prior, alreadyExists := existingByAddr[addr]

		if !alreadyExists {
			insertRows = append(insertRows, row)
			insertedUnits = append(insertedUnits, unit)

			continue
		}

		sameOrdinal := prior.Ordinal.Valid && prior.Ordinal.Int64 == int64(unit.Ordinal)
		sameContent := prior.ContentHash.String == unit.ContentHash

		if sameOrdinal && sameContent {
			// Address, ordinal, and content all unchanged — leave the
			// row untouched.
			continue
		}

		// The address still resolves but ordinal and/or content moved.
		// Upsert the row (new ordinal/content_hash); when the content
		// changed, re-derive edges (all kinds — a unit's edge set derives
		// from the same Text its content hash covers, so a hash turnover
		// is exactly an edge-set turnover) and re-enqueue the embed for
		// leaves (the enqueue loop below skips sections itself).
		reorderRows = append(reorderRows, row)

		if !sameContent {
			insertedUnits = append(insertedUnits, unit)
		}
	}

	// Deleted: addresses in existing \ new.
	var deleteIDs []string

	for addr, row := range existingByAddr {
		if _, kept := newByAddr[addr]; kept {
			continue
		}

		deleteIDs = append(deleteIDs, row.ID)
	}

	// Apply node row changes.
	if len(insertRows) > 0 {
		if upsertErr := sync.Repo.BulkUpsert(insertRows, sync.resolvedSource()); upsertErr != nil {
			return nil, fmt.Errorf("subunit sync: insert rows for %s: %w", fileRow.ID, upsertErr)
		}

		result.Inserted = len(insertRows)
	}

	if len(reorderRows) > 0 {
		if upsertErr := sync.Repo.BulkUpsert(reorderRows, sync.resolvedSource()); upsertErr != nil {
			return nil, fmt.Errorf("subunit sync: reorder rows for %s: %w", fileRow.ID, upsertErr)
		}

		result.Reordered = len(reorderRows)
	}

	if len(deleteIDs) > 0 {
		// Note: a deleted sub-unit's embed_queue entry (if any) is not
		// explicitly removed here. It's harmless — the drainer in
		// internal/embed/drain.go silently skips ErrNodeNotFound — but a
		// debugger reading embed_queue directly may see orphan IDs until
		// the next drain pass clears them.
		if deleteErr := sync.Repo.BulkDelete(deleteIDs); deleteErr != nil {
			return nil, fmt.Errorf("subunit sync: delete rows for %s: %w", fileRow.ID, deleteErr)
		}

		result.Deleted = len(deleteIDs)
	}

	// Re-derive outbound wikilink edges for newly inserted units, batched into
	// one transaction (delete-then-insert per sub-unit) instead of one commit
	// per sub-unit.
	wikilinkEdgeNames := wikilinkEdgeTypeNames(sync.Manifest)

	var edgeBatches []index.EdgeBatch

	for _, unit := range insertedUnits {
		subunitID := subunitRowID(fileRow.ID, unitAddress(unit))
		edges := buildOutboundEdges(unit, subunitID, fileRow.Path, wikilinkEdgeNames)
		edgeBatches = append(edgeBatches, index.EdgeBatch{SourceID: subunitID, SourcePath: fileRow.Path, Edges: edges})
	}

	if upsertErr := sync.EdgeRepo.UpsertAllMany(edgeBatches); upsertErr != nil {
		return nil, fmt.Errorf("subunit sync: write edges for %s: %w", fileRow.ID, upsertErr)
	}

	// Replace the file's contains edges with the current sub-unit set.
	// We always rewrite contains so deletions and inserts both
	// converge to the parser's view of the file. Existing file-level
	// frontmatter edges live under the same (source_id, source_path)
	// pair and would be wiped by EdgeRepo.UpsertAll — so we filter
	// just the contains rows.
	if rewriteErr := sync.rewriteContains(fileRow, units); rewriteErr != nil {
		return nil, rewriteErr
	}

	// Enqueue embedding work for inserted leaves. Sections aggregate
	// from descendants at query time and are not embedded (spec §5.6).
	// Task 4 owns the actual chunker — until then the drainer would
	// read the parent file body for each sub-unit id, which produces
	// an incorrect (but non-crashing) embedding. Tests that exercise
	// the drainer against sub-units are added with Task 4.
	if sync.EmbedQ != nil {
		var leafIDs []string

		for _, unit := range insertedUnits {
			if unit.Kind == KindSection {
				continue
			}

			leafIDs = append(leafIDs, subunitRowID(fileRow.ID, unitAddress(unit)))
		}

		if enqueueErr := sync.EmbedQ.EnqueueMany(leafIDs); enqueueErr != nil {
			return nil, fmt.Errorf("subunit sync: enqueue %s: %w", fileRow.ID, enqueueErr)
		}
	}

	if sync.Logger != nil {
		sync.Logger.Debug("subunit sync apply",
			"file_id", fileRow.ID,
			"inserted", result.Inserted,
			"deleted", result.Deleted,
			"reordered", result.Reordered,
			"units_total", len(units),
		)
	}

	return result, nil
}

// rewriteContains rebuilds the `contains` edges from the file row to its
// current sub-units. It explicitly deletes the file's existing contains
// edges (which the file-level reindex code path does NOT write, so the
// only source is a previous sync pass) and reinserts one row per unit.
//
// We can't use EdgeRepo.UpsertAll for this because UpsertAll deletes
// every edge with (source_id, source_path) regardless of type — that
// would wipe the file's frontmatter-derived edges, which the file-level
// reindex code path owns. Instead we delete by (source_id, type) and
// reinsert via the same edge repo helper.
func (sync *Sync) rewriteContains(fileRow index.NodeRow, units []Unit) error {
	if deleteErr := sync.EdgeRepo.DeleteBySourceAndType(fileRow.ID, containsEdgeType); deleteErr != nil {
		return fmt.Errorf("subunit sync: delete prior contains for %s: %w", fileRow.ID, deleteErr)
	}

	if len(units) == 0 {
		return nil
	}

	rows := make([]index.EdgeRow, 0, len(units))

	for _, unit := range units {
		rows = append(rows, index.EdgeRow{
			Type:       containsEdgeType,
			SourceID:   fileRow.ID,
			TargetID:   subunitRowID(fileRow.ID, unitAddress(unit)),
			SourcePath: fileRow.Path,
			Kind:       structuralEdgeKind,
			Source:     sql.NullString{String: sync.resolvedSource(), Valid: true},
		})
	}

	if insertErr := sync.EdgeRepo.InsertIgnore(rows); insertErr != nil {
		return fmt.Errorf("subunit sync: insert contains for %s: %w", fileRow.ID, insertErr)
	}

	return nil
}

// unitAddress returns the address suffix for a unit's row id: the structural
// address when the kind has one, falling back to the content hash for kinds
// with no address rule.
func unitAddress(unit Unit) string {
	if unit.Address != "" {
		return unit.Address
	}

	return unit.Hash
}

// subunitRowID returns the canonical row id for a sub-unit. The format is
// "<fileID>#<address>", a string that cannot collide with a file id (no file
// id contains `#`).
func subunitRowID(fileID, address string) string {
	return fileID + "#" + address
}

// addressFromID returns the structural-address component of a sub-unit row id
// given its expected parent file id. Returns "" when id does not have the
// "<fileID>#..." shape (i.e., when the row is not a sub-unit of the supplied
// file).
func addressFromID(id, fileID string) string {
	prefix := fileID + "#"

	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return ""
	}

	return id[len(prefix):]
}

// buildSubUnitRow assembles the NodeRow for a single sub-unit. The path,
// mtime, size, and checksum inherit from the parent file row per spec
// §5.3 ("last_mtime is the parent file's mtime").
func buildSubUnitRow(fileRow index.NodeRow, unit Unit, subunitID string) (index.NodeRow, error) {
	propsJSON, marshalErr := json.Marshal(unit.Properties)

	if marshalErr != nil {
		return index.NodeRow{}, fmt.Errorf("subunit sync: marshal properties for %s: %w", subunitID, marshalErr)
	}

	parent := sql.NullString{String: parentRowID(fileRow.ID, unit.ParentAddress), Valid: true}

	return index.NodeRow{
		ID:             subunitID,
		Type:           string(unit.Kind),
		Path:           fileRow.Path,
		Title:          unit.Title,
		PropertiesJSON: string(propsJSON),
		LastMtime:      fileRow.LastMtime,
		LastSize:       fileRow.LastSize,
		LastChecksum:   fileRow.LastChecksum,
		ParentID:       parent,
		Ordinal:        sql.NullInt64{Int64: int64(unit.Ordinal), Valid: true},
		EmbedPayload:   sql.NullString{String: unit.EmbedPayload, Valid: true},
		ContentHash:    sql.NullString{String: unit.ContentHash, Valid: unit.ContentHash != ""},
	}, nil
}

// parentRowID resolves a unit's parent row id. When parentAddress is empty the
// unit lives at the document root, so its parent is the file row itself.
// Otherwise its parent is the enclosing section row identified by
// "<fileID>#<parentAddress>".
func parentRowID(fileID, parentAddress string) string {
	if parentAddress == "" {
		return fileID
	}

	return subunitRowID(fileID, parentAddress)
}

// wikilinkEdgeTypeNames returns the names of every edge type the
// workspace flags with `wikilinks = true`. The order is deterministic
// (lexicographic over edge type names) so DeriveEdges emits the same
// rows on every reindex. The returned slice is empty when no edge type
// opts in.
func wikilinkEdgeTypeNames(loaded *manifest.Manifest) []string {
	if loaded == nil {
		return nil
	}

	var names []string

	for name, edgeType := range loaded.EdgeTypes {
		if edgeType.Wikilinks {
			names = append(names, name)
		}
	}

	// sort.Strings would be tighter but pulling sort in for this one
	// call inflates the imports. A linear insertion-sort is fine for
	// the typical handful-of-edge-types manifest.
	for i := 1; i < len(names); i++ {
		j := i

		for j > 0 && names[j-1] > names[j] {
			names[j-1], names[j] = names[j], names[j-1]
			j--
		}
	}

	return names
}

// buildOutboundEdges runs DeriveEdges against the unit and shapes the
// result into index.EdgeRow values. Targets are emitted with their raw
// wikilink string as TargetID per spec §5.4 — the spec promises
// wikilinks resolve to FILE nodes, and the file id is derived directly
// from the path (no `#` suffix), so the raw target string IS the
// expected file id when the target file exists. Dangling targets land
// on disk as edge rows pointing at missing nodes; the existing
// doctor/dangling-ref machinery handles them.
func buildOutboundEdges(unit Unit, sourceID, sourcePath string, wikilinkEdgeNames []string) []index.EdgeRow {
	specs := DeriveEdges(unit, wikilinkEdgeNames)

	if len(specs) == 0 {
		return nil
	}

	rows := make([]index.EdgeRow, 0, len(specs))

	for _, spec := range specs {
		rows = append(rows, index.EdgeRow{
			Type:       spec.EdgeType,
			SourceID:   sourceID,
			TargetID:   spec.TargetID,
			SourcePath: sourcePath,
			Kind:       "direct",
		})
	}

	return rows
}
