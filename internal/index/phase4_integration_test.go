package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
)

// TestPhase4_UserSectionAndPackSectionCoexist rebuilds a workspace whose
// manifest declares a user-namespace `section` node-type and confirms
// the resulting index holds both a `(file, NULL, section)` row from the
// user-declared file and a `(subunit, markdown, section)` row from the
// markdown sub-document pack. After Phase 4 Task 2 rescoping the two
// reservations live in different (kind, source) slices and must coexist.
func TestPhase4_UserSectionAndPackSectionCoexist(test *testing.T) {
	test.Parallel()

	root := test.TempDir()

	manifestText := `[workspace]
name = "test"
sub-units = true

[node-types.section]
properties = [
    { name = "summary", type = "string" },
]
`
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestText), 0o644); writeErr != nil {
		test.Fatalf("seed manifest: %v", writeErr)
	}

	// User-declared section node (source = NULL, kind = file, type = section).
	userSection := `---
type: section
title: Overview
summary: hi
---
`
	if writeErr := os.WriteFile(filepath.Join(root, "section-overview.md"), []byte(userSection), 0o644); writeErr != nil {
		test.Fatalf("seed user section: %v", writeErr)
	}

	// File with a markdown heading so the sub-unit pipeline emits a
	// (subunit, markdown, section) row.
	noteText := `---
type: note
title: Standup
---
# Section A

Body.
`
	if writeErr := os.WriteFile(filepath.Join(root, "standup.md"), []byte(noteText), 0o644); writeErr != nil {
		test.Fatalf("seed note: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(filepath.Join(root, "tusk.toml"))
	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	manifest.MergeBuiltinPacks(loaded)

	if len(loaded.SubUnitConflicts) != 0 {
		test.Fatalf("unexpected SubUnitConflicts after rescoping: %+v", loaded.SubUnitConflicts)
	}

	indexPath := filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	// Seed a stale DB so OpenOrRebuild trips the schema-version mismatch
	// and exercises the rebuild path end-to-end.
	stale, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}
	if setErr := index.NewMetaRepo(stale).Set(index.MetaSchemaVersionKey, "stale-version"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}
	if closeErr := stale.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	store, openErr := indexopen.OpenOrRebuild(context.Background(), indexopen.Config{
		IndexPath: indexPath,
		ReindexFactory: func(idx *index.Index) reindex.Config {
			return reindex.Config{
				Root:      root,
				Repo:      index.NewNodeRepo(idx),
				Edges:     index.NewEdgeRepo(idx),
				EdgeTypes: loaded.EdgeTypes,
				NodeTypes: loaded.NodeTypes,
				Manifest:  loaded,
			}
		},
		Logger: func(string) {},
	})
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	var userSectionCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='file' AND source IS NULL AND type='section'`).Scan(&userSectionCount); scanErr != nil {
		test.Fatalf("user section count: %v", scanErr)
	}
	if userSectionCount == 0 {
		test.Error("expected user-declared (file, NULL, section) row")
	}

	var packSectionCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='subunit' AND source='markdown' AND type='section'`).Scan(&packSectionCount); scanErr != nil {
		test.Fatalf("pack section count: %v", scanErr)
	}
	if packSectionCount == 0 {
		test.Error("expected pack-derived (subunit, markdown, section) row")
	}
}
