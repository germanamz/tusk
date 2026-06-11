package reindex_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

func indexHTMLWithSubUnits(test *testing.T, relPath, htmlBody string) (*index.NodeRepo, *index.EdgeRepo, *index.Index) {
	test.Helper()

	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, relPath), []byte(htmlBody), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	test.Cleanup(func() { _ = store.Close() })

	repo := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	fileStates := index.NewFileStateRepo(store)
	meta := index.NewMetaRepo(store)

	// A hand-built Manifest with Meta==nil has SubUnitsEnabled()==true by
	// default; MergeBuiltinPacks splices the subdocument + html packs
	// (Phase 2) so NodeTypes/EdgeTypes carry the reserved sub-unit decls.
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

	return repo, edges, store
}

func TestHTMLSubUnits_KindsAddressesAndContainsEdges(test *testing.T) {
	htmlBody := "<html><head>" +
		"<meta name=\"tusk:type\" content=\"note\">" +
		"</head><body>" +
		"<h1>Top</h1><p>Alpha.</p>" +
		"<h2>Sub</h2><p>Beta.</p>" +
		"</body></html>"

	repo, edges, _ := indexHTMLWithSubUnits(test, "page.html", htmlBody)

	subUnits, listErr := repo.ListSubUnitsForFile("page.html")

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	gotKinds := map[string]int{}

	for _, sub := range subUnits {
		gotKinds[sub.Type]++

		if !strings.HasPrefix(sub.ID, "page.html#") {
			test.Errorf("sub-unit id %q lacks expected file prefix", sub.ID)
		}
	}

	if gotKinds["section"] != 2 {
		test.Errorf("section count = %d, want 2 (h1 + nested h2)", gotKinds["section"])
	}

	if gotKinds["paragraph"] != 2 {
		test.Errorf("paragraph count = %d, want 2", gotKinds["paragraph"])
	}

	contains, edgeErr := edges.ListBySource("page.html")

	if edgeErr != nil {
		test.Fatalf("ListBySource: %v", edgeErr)
	}

	containsCount := 0

	for _, edge := range contains {
		if edge.Type != "contains" {
			continue
		}

		containsCount++

		if !edge.Source.Valid || edge.Source.String != "html" {
			test.Errorf("contains edge %s: source = %+v, want {html true}", edge.TargetID, edge.Source)
		}
	}

	if containsCount != len(subUnits) {
		test.Errorf("contains edges = %d, want one per sub-unit (%d)", containsCount, len(subUnits))
	}
}

func TestHTMLSubUnits_ReindexIsIdempotent(test *testing.T) {
	root := test.TempDir()

	htmlBody := "<html><head>" +
		"<meta name=\"tusk:type\" content=\"note\">" +
		"</head><body><h1>Top</h1><p>Alpha.</p></body></html>"

	htmlPath := filepath.Join(root, "page.html")

	if writeErr := os.WriteFile(htmlPath, []byte(htmlBody), 0o644); writeErr != nil {
		test.Fatalf("write page.html: %v", writeErr)
	}

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	fileStates := index.NewFileStateRepo(store)
	meta := index.NewMetaRepo(store)

	// A hand-built Manifest with Meta==nil has SubUnitsEnabled()==true by
	// default; MergeBuiltinPacks splices the subdocument + html packs
	// (Phase 2) so NodeTypes/EdgeTypes carry the reserved sub-unit decls.
	loaded := &manifest.Manifest{}
	manifest.MergeBuiltinPacks(loaded)

	drain := func() reindex.DrainReport {
		report, runErr := reindex.Run(reindex.Config{
			Root: root, Repo: repo, EmbedQueue: queueRepo, Meta: meta,
			FileStates: fileStates, Async: true,
		})

		if runErr != nil {
			test.Fatalf("Run: %v", runErr)
		}

		drainReport, drainErr := reindex.DrainReindexQueue(context.Background(), reindex.WorkerConfig{
			Root: root, Repo: repo, Edges: edges, EmbedQueue: queueRepo,
			FileStates: fileStates, Manifest: loaded, NodeTypes: loaded.NodeTypes,
			EdgeTypes: loaded.EdgeTypes, Workers: 1, TTL: time.Minute,
			Generation: report.Generation,
		})

		if drainErr != nil {
			test.Fatalf("DrainReindexQueue: %v", drainErr)
		}

		return drainReport
	}

	drain()

	firstPass, _ := repo.ListSubUnitsForFile("page.html")

	second := drain()

	secondPass, _ := repo.ListSubUnitsForFile("page.html")

	if len(firstPass) != len(secondPass) {
		test.Errorf("sub-unit count changed across re-index: %d -> %d", len(firstPass), len(secondPass))
	}

	if second.SubUnitsInserted != 0 || second.SubUnitsDeleted != 0 || second.SubUnitsReordered != 0 {
		test.Errorf("second pass churn = %+v, want all zero (idempotent)", second)
	}
}

func TestHTMLSubUnits_MarkdownAndHTMLSectionsCoexist(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(
		filepath.Join(root, "note.md"),
		[]byte("---\ntype: note\n---\n\n# Shared Heading\n\nMarkdown prose.\n"),
		0o644,
	); writeErr != nil {
		test.Fatalf("write note.md: %v", writeErr)
	}

	if writeErr := os.WriteFile(
		filepath.Join(root, "note.html"),
		[]byte("<html><head><meta name=\"tusk:type\" content=\"note\"></head>"+
			"<body><h1>Shared Heading</h1><p>HTML prose.</p></body></html>"),
		0o644,
	); writeErr != nil {
		test.Fatalf("write note.html: %v", writeErr)
	}

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	fileStates := index.NewFileStateRepo(store)
	meta := index.NewMetaRepo(store)

	// A hand-built Manifest with Meta==nil has SubUnitsEnabled()==true by
	// default; MergeBuiltinPacks splices the subdocument + html packs
	// (Phase 2) so NodeTypes/EdgeTypes carry the reserved sub-unit decls.
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

	// Distinct file ids: markdown strips .md, html retains .html.
	mdSubs, _ := repo.ListSubUnitsForFile("note")
	htmlSubs, _ := repo.ListSubUnitsForFile("note.html")

	if len(mdSubs) == 0 {
		test.Fatalf("markdown note produced no sub-units")
	}

	if len(htmlSubs) == 0 {
		test.Fatalf("html note produced no sub-units")
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
	assertSource(htmlSubs, "html")

	// Ids never collide: every markdown sub-unit id is prefixed "note#",
	// every html sub-unit id is prefixed "note.html#" — disjoint sets.
	for _, md := range mdSubs {
		if strings.HasPrefix(md.ID, "note.html#") {
			test.Errorf("markdown sub-unit %q collides into html namespace", md.ID)
		}
	}
}
