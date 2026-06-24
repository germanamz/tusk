package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// TestUserAndPackSectionTypesCoexist rebuilds a workspace whose manifest
// declares a user-namespace `section` node-type and confirms the
// resulting index holds both a `(file, NULL, section)` row from the
// user-declared file and a `(subunit, markdown, section)` row from the
// markdown sub-document pack. The reservation system scopes user and
// pack declarations into separate (kind, source) slices so identically
// named types coexist without conflict.
func TestUserAndPackSectionTypesCoexist(test *testing.T) {
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

	indexPath := filepath.Join(root, ".tusk", "index.db")

	store := openRebuilt(test, indexPath, func(idx *index.Index) reindex.Config {
		return reindex.Config{
			Root:       root,
			Repo:       index.NewNodeRepo(idx),
			Edges:      index.NewEdgeRepo(idx),
			EdgeTypes:  loaded.EdgeTypes,
			NodeTypes:  loaded.NodeTypes,
			Manifest:   loaded,
			Meta:       index.NewMetaRepo(idx),
			FileStates: index.NewFileStateRepo(idx),
			EmbedQueue: index.NewEmbedQueueRepo(idx),
		}
	})
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
