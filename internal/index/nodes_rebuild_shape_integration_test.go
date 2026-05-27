package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// TestRebuildPreservesNodeKindAndSource rebuilds a workspace from
// scratch through OpenOrRebuild and asserts every node row carries
// valid kind/source values: file rows have source=NULL, sub-units
// have source='markdown', and at least one of each kind is produced.
func TestRebuildPreservesNodeKindAndSource(test *testing.T) {
	test.Parallel()

	root := test.TempDir()

	noteWithSections := `---
type: note
title: Standup
---
# Section One

Paragraph one.

# Section Two

Paragraph two.
`
	if writeErr := os.WriteFile(filepath.Join(root, "standup.md"), []byte(noteWithSections), 0o644); writeErr != nil {
		test.Fatalf("seed file: %v", writeErr)
	}

	indexPath := filepath.Join(root, ".tusk", "index.db")

	store := openRebuilt(test, indexPath, func(idx *index.Index) reindex.Config {
		return reindex.Config{
			Root:       root,
			Repo:       index.NewNodeRepo(idx),
			Edges:      index.NewEdgeRepo(idx),
			Manifest:   &manifest.Manifest{},
			Meta:       index.NewMetaRepo(idx),
			FileStates: index.NewFileStateRepo(idx),
		}
	})
	defer store.Close()

	// Every row has a valid kind value.
	var nullKindCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind IS NULL`).Scan(&nullKindCount); scanErr != nil {
		test.Fatalf("kind null count: %v", scanErr)
	}
	if nullKindCount != 0 {
		test.Errorf("found %d rows with NULL kind", nullKindCount)
	}

	// File rows have source=NULL; sub-units have source='markdown'.
	var fileBadCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind = 'file' AND source IS NOT NULL`).Scan(&fileBadCount); scanErr != nil {
		test.Fatalf("file source count: %v", scanErr)
	}
	if fileBadCount != 0 {
		test.Errorf("found %d file rows with non-NULL source", fileBadCount)
	}

	var subUnitBadCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind = 'subunit' AND (source IS NULL OR source != 'markdown')`).Scan(&subUnitBadCount); scanErr != nil {
		test.Fatalf("subunit source count: %v", scanErr)
	}
	if subUnitBadCount != 0 {
		test.Errorf("found %d sub-unit rows with wrong source", subUnitBadCount)
	}

	// At least one of each kind exists.
	var fileCount, subUnitCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='file'`).Scan(&fileCount); scanErr != nil {
		test.Fatalf("file count: %v", scanErr)
	}
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='subunit'`).Scan(&subUnitCount); scanErr != nil {
		test.Fatalf("subunit count: %v", scanErr)
	}
	if fileCount == 0 {
		test.Error("expected at least one file row")
	}
	if subUnitCount == 0 {
		test.Error("expected at least one subunit row")
	}
}
