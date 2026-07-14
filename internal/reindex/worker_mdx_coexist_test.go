package reindex_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// TestReindex_MarkdownAndMDXCoexist walks a same-stem note.md + note.mdx pair
// through the real reindex.Run + DrainReindexQueue pipeline and proves the
// feature's central invariant end-to-end: the two files persist as DISTINCT
// nodes (markdown strips ".md" -> id "note"; the markdown-twin .mdx retains its
// extension -> id "note.mdx") so they never collide on the nodes.id primary key.
// It also confirms .mdx routes to the markdown sub-unit pipeline (source
// "markdown", NOT "html"), including a JSX tag in the body that markdown parses
// as raw HTML without derailing the split. Mirrors the md-vs-html precedent in
// TestHTMLSubUnits_MarkdownAndHTMLSectionsCoexist.
func TestReindex_MarkdownAndMDXCoexist(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(
		filepath.Join(root, "note.md"),
		[]byte("---\ntype: note\ntitle: Plain\n---\n\n# Shared Heading\n\nMarkdown prose.\n"),
		0o644,
	); writeErr != nil {
		test.Fatalf("write note.md: %v", writeErr)
	}

	if writeErr := os.WriteFile(
		filepath.Join(root, "note.mdx"),
		[]byte("---\ntype: note\ntitle: MDX\n---\n\nimport { Callout } from './c'\n\n"+
			"# Shared Heading\n\n<Callout>MDX prose about tokens.</Callout>\n"),
		0o644,
	); writeErr != nil {
		test.Fatalf("write note.mdx: %v", writeErr)
	}

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	fileStates := index.NewFileStateRepo(store)
	meta := index.NewMetaRepo(store)

	loaded := &manifest.Manifest{}
	manifest.MergeBuiltinPacks(loaded)

	report, runErr := reindex.Run(reindex.Config{
		Root: root, Repo: repo, EmbedQueue: queueRepo, Meta: meta,
		FileStates: fileStates, Async: true,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if _, drainErr := reindex.DrainReindexQueue(context.Background(), reindex.WorkerConfig{
		Root: root, Repo: repo, Edges: edges, EmbedQueue: queueRepo,
		FileStates: fileStates, Manifest: loaded, NodeTypes: loaded.NodeTypes,
		EdgeTypes: loaded.EdgeTypes, Workers: 1, TTL: time.Minute,
		Generation: report.Generation,
	}); drainErr != nil {
		test.Fatalf("DrainReindexQueue: %v", drainErr)
	}

	// Two distinct file-level nodes — the collision this feature must avoid.
	mdNode, mdErr := repo.Get("note")

	if mdErr != nil {
		test.Fatalf("markdown node id \"note\" not indexed: %v", mdErr)
	}

	if mdNode.Path != "note.md" {
		test.Errorf("markdown node path = %q, want note.md", mdNode.Path)
	}

	mdxNode, mdxErr := repo.Get("note.mdx")

	if mdxErr != nil {
		test.Fatalf("mdx node id \"note.mdx\" not indexed: %v", mdxErr)
	}

	if mdxNode.Path != "note.mdx" {
		test.Errorf("mdx node path = %q, want note.mdx", mdxNode.Path)
	}

	// Guard the collision path explicitly: the bare stem "note" must NOT resolve
	// to the mdx file, and there must be no phantom bare-stem alias of the mdx.
	if mdNode.Path == mdxNode.Path {
		test.Fatalf("markdown and mdx nodes share a path %q — the ids collided", mdNode.Path)
	}

	// Both split into sub-units, and the mdx ones are markdown-sourced.
	mdSubs, _ := repo.ListSubUnitsForFile("note")
	mdxSubs, _ := repo.ListSubUnitsForFile("note.mdx")

	if len(mdSubs) == 0 {
		test.Fatalf("markdown note produced no sub-units")
	}

	if len(mdxSubs) == 0 {
		test.Fatalf("mdx note produced no sub-units")
	}

	assertSource := func(rows []index.NodeRow, want string) {
		for _, row := range rows {
			var source string

			if scanErr := store.DB().QueryRow(`SELECT source FROM nodes WHERE id = ?`, row.ID).Scan(&source); scanErr != nil {
				test.Fatalf("scan source for %s: %v", row.ID, scanErr)
			}

			if source != want {
				test.Errorf("row %s: source = %q, want %q", row.ID, source, want)
			}
		}
	}

	assertSource(mdSubs, "markdown")
	assertSource(mdxSubs, "markdown")

	// Ids never collide: markdown sub-unit ids are prefixed "note#", mdx sub-unit
	// ids are prefixed "note.mdx#" — disjoint sets.
	for _, md := range mdSubs {
		if strings.HasPrefix(md.ID, "note.mdx#") {
			test.Errorf("markdown sub-unit %q collides into mdx namespace", md.ID)
		}
	}

	for _, mdx := range mdxSubs {
		if !strings.HasPrefix(mdx.ID, "note.mdx#") {
			test.Errorf("mdx sub-unit %q not namespaced under note.mdx#", mdx.ID)
		}
	}

	// A second pass must converge: no churn, ids stable.
	report2, _ := reindex.Run(reindex.Config{
		Root: root, Repo: repo, EmbedQueue: queueRepo, Meta: meta,
		FileStates: fileStates, Async: true,
	})

	if _, drainErr := reindex.DrainReindexQueue(context.Background(), reindex.WorkerConfig{
		Root: root, Repo: repo, Edges: edges, EmbedQueue: queueRepo,
		FileStates: fileStates, Manifest: loaded, NodeTypes: loaded.NodeTypes,
		EdgeTypes: loaded.EdgeTypes, Workers: 1, TTL: time.Minute,
		Generation: report2.Generation,
	}); drainErr != nil {
		test.Fatalf("second DrainReindexQueue: %v", drainErr)
	}

	if _, mdErr := repo.Get("note"); mdErr != nil {
		test.Errorf("markdown node \"note\" vanished on reconverge: %v", mdErr)
	}

	if _, mdxErr := repo.Get("note.mdx"); mdxErr != nil {
		test.Errorf("mdx node \"note.mdx\" vanished on reconverge: %v", mdxErr)
	}

	// No phantom bare-stem alias of the mdx should ever appear.
	if _, phantomErr := repo.Get("note.mdx.md"); !errors.Is(phantomErr, index.ErrNodeNotFound) {
		test.Errorf("unexpected phantom node note.mdx.md, err = %v", phantomErr)
	}
}
