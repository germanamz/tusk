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

// TestPhase3_AllThreeEdgeKindsPresentAfterRebuild rebuilds a workspace
// from scratch and asserts every edge writer path landed at least one
// row carrying the kind/source values pinned by the Phase 3 CHECK.
//
//   - direct     ← frontmatter `mentions: [...]` (edge-type declared,
//     not a ref-property).
//   - derived    ← frontmatter `tags: [...]` (matches the note type's
//     `tags` ref-property; manifest synthesizes the edge type).
//   - structural ← sub-unit `contains` edges from the section heading
//     in the seed note.
func TestPhase3_AllThreeEdgeKindsPresentAfterRebuild(test *testing.T) {
	test.Parallel()

	root := test.TempDir()

	manifestText := `[workspace]
name = "test"
sub-units = true

[node-types.tag]

[node-types.note]
properties = [
    { name = "tags", type = "list-of", item-type = "ref", to = "tag" },
]

[edge-types.mentions]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
`
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestText), 0o644); writeErr != nil {
		test.Fatalf("seed manifest: %v", writeErr)
	}

	noteText := `---
type: note
title: Standup
mentions:
  - people/dana
tags:
  - "[[tags/retro]]"
---
# Section A

Body text.
`
	// File name sorts after `tags/` so the walker indexes the tag (ref
	// target) before the note. Without this ordering the wikilink lookup
	// for [[tags/retro]] fires before the tag exists and the ref edge is
	// cleared as dangling, leaving no derived row in the table.
	if writeErr := os.WriteFile(filepath.Join(root, "zz-standup.md"), []byte(noteText), 0o644); writeErr != nil {
		test.Fatalf("seed note: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, "people"), 0o755); mkErr != nil {
		test.Fatalf("mkdir people: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "people", "dana.md"), []byte("---\ntype: note\ntitle: Dana\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed dana: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, "tags"), 0o755); mkErr != nil {
		test.Fatalf("mkdir tags: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "tags", "retro.md"), []byte("---\ntype: tag\ntitle: Retro\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed tag: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(filepath.Join(root, "tusk.toml"))
	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	manifest.MergeBuiltinPacks(loaded)

	indexPath := filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	// Seed a stale DB so OpenOrRebuild trips the schema-version mismatch
	// and runs the full reindex path (mirrors phase2_integration_test.go).
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
				Root:          root,
				Repo:          index.NewNodeRepo(idx),
				Edges:         index.NewEdgeRepo(idx),
				EdgeTypes:     loaded.EdgeTypes,
				NodeTypes:     loaded.NodeTypes,
				Manifest:      loaded,
				PropertyDrift: index.NewPropertyDriftRepo(idx),
			}
		},
		Logger: func(string) {},
	})
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	rows, queryErr := store.DB().Query(`SELECT kind, COUNT(*) FROM edges GROUP BY kind`)
	if queryErr != nil {
		test.Fatalf("query: %v", queryErr)
	}
	defer rows.Close()

	counts := map[string]int{}

	for rows.Next() {
		var (
			kind  string
			count int
		)

		if scanErr := rows.Scan(&kind, &count); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}

		counts[kind] = count
	}

	if counts["direct"] == 0 {
		test.Error("expected at least one direct edge (from mentions)")
	}

	if counts["derived"] == 0 {
		test.Error("expected at least one derived edge (from tags references)")
	}

	if counts["structural"] == 0 {
		test.Error("expected at least one structural edge (contains/contained-by)")
	}
}
