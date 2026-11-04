package reindex_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// TestRun_LeafHashesAgreeWithEmbeddedText pins #862's two halves together:
// the sync folds a leaf's context into nodes.content_hash from the parsed
// units, and the drain builds the text it embeds from the stored rows. With
// no document prefix, the drain hashes exactly header+payload, so the two
// hashes match only if both sides built the same context, headings included.
func TestRun_LeafHashesAgreeWithEmbeddedText(test *testing.T) {
	for _, mode := range []string{manifest.SubUnitContextTitle, manifest.SubUnitContextPath} {
		test.Run(mode, func(test *testing.T) {
			vault := newSettingsVault(test)

			writeNode(test, vault.root, "b.md", "type: note\ntitle: B\naliases: [bee]\n",
				"# B\n\nIntro.\n\n## Async\n\n### Detail\n\nDeep paragraph.\n\n- a list item\n")

			section := nomic()
			section.SubUnitContext = mode
			section.SubUnitProperties = []string{"aliases"}

			vault.run(test, section, false)

			nodes := index.NewNodeRepo(vault.store)

			leaves, listErr := nodes.ListSubUnitsForFile("b")

			if listErr != nil {
				test.Fatalf("ListSubUnitsForFile: %v", listErr)
			}

			checked := 0

			for _, leaf := range leaves {
				if leaf.Type == "section" {
					continue
				}

				vectors, getErr := vault.embeddings.GetByNodeID(leaf.ID)

				if getErr != nil || len(vectors) != 1 {
					test.Fatalf("%s: %d vectors (err %v), want 1", leaf.ID, len(vectors), getErr)
				}

				if vectors[0].ContentHash != leaf.ContentHash.String {
					test.Errorf("%s: embedded text hash %s, nodes.content_hash %s; the sync and the drain built different contexts",
						leaf.ID, vectors[0].ContentHash, leaf.ContentHash.String)
				}

				checked++
			}

			if checked != 3 {
				test.Errorf("checked %d leaves, want 3", checked)
			}
		})
	}
}
