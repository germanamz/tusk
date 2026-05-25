package reindex_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/reindex"
)

// integrationStubEmbedder produces a deterministic vector keyed off the
// payload prefix so the same payload always returns the same vector and
// distinct payloads cluster differently. Used by the Phase 2 acceptance
// test instead of a real Ollama embedder.
type integrationStubEmbedder struct {
	dim   int
	calls int
}

func (stub *integrationStubEmbedder) Embed(_ context.Context, payload []byte) ([]float32, error) {
	stub.calls++

	out := make([]float32, stub.dim)

	if len(payload) == 0 {
		return out, nil
	}

	// First byte controls which slot is "hot" so we can craft fixture
	// payloads that the cosine ranker can distinguish. The remaining
	// slots get a tiny seasoning so vectors are not degenerate zeros.
	out[int(payload[0])%stub.dim] = 1

	for idx := 1; idx < len(payload) && idx < stub.dim; idx++ {
		out[idx] = float32(payload[idx]) / 1024.0
	}

	return out, nil
}

func (stub *integrationStubEmbedder) Model() string { return "integration-stub" }
func (stub *integrationStubEmbedder) Dim() int      { return stub.dim }

// TestPhase2Integration is the Phase 2 acceptance test (plan Task 6).
// It exercises the full sub-unit-aware pipeline end to end: reindex with
// the sub-unit pack on, structural query for an open todo, semantic
// query against a stub-embedded vault, and the doctor sub-unit pane.
//
// The vault is intentionally small but exercises every kind the AST
// emits: sections, paragraphs, list items (with a checkbox), a code
// block, a blockquote, a table, and a wikilink.
func TestPhase2Integration(test *testing.T) {
	root := test.TempDir()

	// File 1: headings + paragraphs (sections + paragraphs). The H3
	// `### Detail` exists so the heading-level<=2 narrowing assertion
	// has something to exclude.
	writeNode(test, root, "notes/long.md", "type: note\ntitle: Long\n",
		"# Heading\n\nFirst paragraph of the long note.\n\n## Sub\n\nSecond paragraph under the sub section.\n\n### Detail\n\nThird-level paragraph that the heading-level<=2 filter must skip.\n")

	// File 2: task list + table (list-items + table-cells), plus an
	// open todo we can target with a direct sub-unit query.
	writeNode(test, root, "notes/tasks.md", "type: note\ntitle: Tasks\n",
		"# Open work\n\n- [ ] write the integration test\n- [x] ship the doctor pane\n\n| Owner | State |\n| --- | --- |\n| Alice | done |\n| Bob | open |\n")

	// File 3: wikilink + code block + blockquote (exercises code-block
	// and blockquote AST kinds, plus the wikilink edge materialization).
	writeNode(test, root, "notes/wikilink.md", "type: note\ntitle: WL\n",
		"see [[notes/long]] for the heading.\n\n```go\nfmt.Println(\"hi\")\n```\n\n> a calm quote about subunits\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddings := index.NewEmbeddingRepo(store)
	embedder := &integrationStubEmbedder{dim: 16}

	// Hand-built manifest: defaults to sub-units enabled (Meta is nil
	// → SubUnitsEnabled() returns true). MergeBuiltinPacks installs
	// the six sub-unit node types and the contains edge so the query
	// validator accepts `type=section`, `type=list-item`, etc.
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{
			"references": manifest.EdgeType{
				From: []string{"*"}, To: []string{"*"},
				Cardinality: manifest.CardinalityManyToMany,
				Wikilinks:   true,
			},
		},
	}

	manifest.MergeBuiltinPacks(loaded)

	cfg := reindex.Config{
		Root:          root,
		Repo:          nodes,
		Edges:         edges,
		EdgeTypes:     loaded.EdgeTypes,
		Manifest:      loaded,
		EmbedQueue:    queueRepo,
		EmbeddingRepo: embeddings,
		Embedder:      embedder,
		Chunker:       embed.WholeDocument{},
	}

	report, runErr := reindex.Run(cfg)

	if runErr != nil {
		test.Fatalf("reindex.Run: %v", runErr)
	}

	if report.Indexed != 3 {
		test.Errorf("Indexed = %d, want 3", report.Indexed)
	}

	if report.SubUnitsInserted == 0 {
		test.Fatalf("SubUnitsInserted = 0, want >0")
	}

	// --- Sub-unit row counts per file --------------------------------

	totalSubUnits := 0
	totalLeafSubUnits := 0
	kindTotals := map[string]int{}

	for _, fileID := range []string{"notes/long", "notes/tasks", "notes/wikilink"} {
		fileSub, listErr := nodes.ListSubUnitsForFile(fileID)

		if listErr != nil {
			test.Fatalf("ListSubUnitsForFile %s: %v", fileID, listErr)
		}

		if len(fileSub) == 0 {
			test.Errorf("%s: no sub-unit rows", fileID)
		}

		for _, row := range fileSub {
			totalSubUnits++
			kindTotals[row.Type]++

			if row.Type != "section" {
				totalLeafSubUnits++
			}
		}
	}

	// Required kinds per fixture. Sections come from the H1/H2 in
	// notes/long and notes/tasks; the H1 in notes/wikilink adds none
	// (no heading in that file).
	for _, kind := range []string{"section", "paragraph", "list-item", "code-block", "blockquote", "table-cell"} {
		if kindTotals[kind] == 0 {
			test.Errorf("expected at least one sub-unit row of kind %q, got %v", kind, kindTotals)
		}
	}

	// --- contains edges -------------------------------------------------

	containsCount := 0

	for _, fileID := range []string{"notes/long", "notes/tasks", "notes/wikilink"} {
		listed, _ := edges.ListBySource(fileID)

		for _, edge := range listed {
			if edge.Type == "contains" {
				containsCount++
			}
		}
	}

	if containsCount != totalSubUnits {
		test.Errorf("contains edges = %d, want %d (one per sub-unit)", containsCount, totalSubUnits)
	}

	// --- embeddings: one row per LEAF sub-unit (sections excluded) ----
	//
	// reindex.Run enqueues both file-level rows and the sub-unit sync's
	// leaf-only enqueues. The drain produces one embedding row per
	// pending id; for sub-unit rows that's exactly the leaf count, for
	// file rows it's one per indexed file. Section sub-units are
	// excluded from the sub-unit enqueue path by design (sections are
	// aggregated from their descendants at query time, never embedded
	// directly).

	leafEmbeddingsExpected := totalLeafSubUnits + report.Indexed

	var embeddingCount int

	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&embeddingCount); scanErr != nil {
		test.Fatalf("count embeddings: %v", scanErr)
	}

	if embeddingCount != leafEmbeddingsExpected {
		test.Errorf("embeddings = %d, want %d (one per leaf sub-unit + one per file row)",
			embeddingCount, leafEmbeddingsExpected)
	}

	// Sub-unit-only embedding count must equal the leaf count exactly:
	// no section row should ever land in the embeddings table.
	var sectionEmbeddingCount int

	if scanErr := store.DB().QueryRow(`
		SELECT COUNT(*) FROM embeddings e
		JOIN nodes n ON n.id = e.node_id
		WHERE n.type = 'section'
	`).Scan(&sectionEmbeddingCount); scanErr != nil {
		test.Fatalf("count section embeddings: %v", scanErr)
	}

	if sectionEmbeddingCount != 0 {
		test.Errorf("section embeddings = %d, want 0 (sections are aggregated, not embedded)",
			sectionEmbeddingCount)
	}

	if embedder.calls == 0 {
		test.Errorf("stub embedder never called")
	}

	// --- direct sub-unit query: open todos -----------------------------

	deps := query.Deps{
		Database: store.DB(),
		Manifest: loaded,
		Nodes:    nodes,
	}

	// Direct sub-unit query: the open todo `[ ] write the integration
	// test`. The filter compiler coerces the bareword `false` to the
	// integer 0 so the comparison matches SQLite's json_extract of a
	// JSON boolean — without this, the filter would return zero rows.
	todoResult, todoErr := query.Run(context.Background(), deps, query.Request{
		Filter: "type=list-item AND checkbox=false",
	})

	if todoErr != nil {
		test.Fatalf("query (open todos): %v", todoErr)
	}

	if len(todoResult.Rows) != 1 {
		test.Fatalf("checkbox=false query returned %d rows, want exactly 1 (the open todo)", len(todoResult.Rows))
	}

	openTodo := todoResult.Rows[0]

	if openTodo.Type != "list-item" {
		test.Errorf("open todo type = %q, want list-item", openTodo.Type)
	}

	if openTodo.ParentID == "" {
		test.Errorf("open todo %s missing parent_id", openTodo.ID)
	}

	if !strings.Contains(strings.ToLower(openTodo.Title), "integration test") {
		test.Errorf("open todo title = %q, want substring 'integration test'", openTodo.Title)
	}

	// --- heading-level narrowing ---------------------------------------

	headingResult, headingErr := query.Run(context.Background(), deps, query.Request{
		Filter: "type=section AND heading-level<=2",
	})

	if headingErr != nil {
		test.Fatalf("query (heading-level<=2): %v", headingErr)
	}

	if len(headingResult.Rows) == 0 {
		test.Fatalf("heading-level<=2 returned no rows; expected H1 + H2 sections")
	}

	sawH1 := false
	sawH2 := false

	for _, row := range headingResult.Rows {
		if row.Type != "section" {
			test.Errorf("row %s type = %q, want section", row.ID, row.Type)
		}

		full, getErr := nodes.Get(row.ID)

		if getErr != nil {
			test.Errorf("nodes.Get %s: %v", row.ID, getErr)
			continue
		}

		switch {
		case strings.Contains(full.PropertiesJSON, `"heading-level":1`):
			sawH1 = true
		case strings.Contains(full.PropertiesJSON, `"heading-level":2`):
			sawH2 = true
		case strings.Contains(full.PropertiesJSON, `"heading-level":3`):
			test.Errorf("heading-level<=2 must exclude H3 section %s; properties=%s", row.ID, full.PropertiesJSON)
		}
	}

	if !sawH1 || !sawH2 {
		test.Errorf("heading-level<=2 expected at least one H1 and one H2; got H1=%v H2=%v", sawH1, sawH2)
	}

	// --- semantic query: file-grouped result with matched_units -------

	semanticDeps := query.Deps{
		Database:   store.DB(),
		Manifest:   loaded,
		Nodes:      nodes,
		Embedder:   embedder,
		Embeddings: embeddings,
	}

	semanticResult, semErr := query.Run(context.Background(), semanticDeps, query.Request{
		Filter:   "type=note",
		Semantic: "heading paragraph",
	})

	if semErr != nil {
		test.Fatalf("semantic query: %v", semErr)
	}

	if semanticResult.Semantic == nil {
		test.Fatalf("expected Semantic result")
	}

	if len(semanticResult.Semantic.Ranked) == 0 {
		test.Fatalf("semantic ranker returned no rows")
	}

	// At least one ranked file must carry MatchedUnits (the semantic
	// path groups leaf hits under their parent file).
	sawMatchedUnits := false

	for _, file := range semanticResult.Semantic.Ranked {
		if len(file.MatchedUnits) > 0 {
			sawMatchedUnits = true
			break
		}
	}

	if !sawMatchedUnits {
		test.Errorf("no ranked file carries matched_units; want grouped-by-parent shape")
	}

	// --- doctor sub-unit pane ------------------------------------------

	doctorReport, doctorErr := doctor.Run(doctor.Config{
		Nodes:      nodes,
		Edges:      edges,
		EmbedQueue: queueRepo,
		Embeddings: embeddings,
		Manifest:   loaded,
	})

	if doctorErr != nil {
		test.Fatalf("doctor.Run: %v", doctorErr)
	}

	pane := doctorReport.SubUnitPane

	if pane == nil {
		test.Fatalf("doctor SubUnitPane = nil; want populated")
	}

	if pane.Total != totalSubUnits {
		test.Errorf("pane Total = %d, want %d", pane.Total, totalSubUnits)
	}

	for kind, want := range kindTotals {
		if got := pane.CountByKind[kind]; got != want {
			test.Errorf("pane CountByKind[%q] = %d, want %d", kind, got, want)
		}
	}

	if pane.OrphanedSubUnits != 0 {
		test.Errorf("pane OrphanedSubUnits = %d, want 0", pane.OrphanedSubUnits)
	}

	// Drain flushed the queue; nothing pending after a clean pass.
	if pane.EmbedQueueFiles != 0 || pane.EmbedQueueSubUnits != 0 {
		test.Errorf("pane queue counts = (files=%d, sub-units=%d), want (0, 0) after drain",
			pane.EmbedQueueFiles, pane.EmbedQueueSubUnits)
	}

	if pane.OversizeEmbedPayloads != 0 {
		test.Errorf("pane OversizeEmbedPayloads = %d, want 0", pane.OversizeEmbedPayloads)
	}

	// Wikilink edge: at least one sub-unit row from notes/wikilink
	// should carry a references edge → notes/long.
	wikiRows, _ := nodes.ListSubUnitsForFile("notes/wikilink")
	sawWikilink := false

	for _, row := range wikiRows {
		listed, _ := edges.ListBySource(row.ID)

		for _, edge := range listed {
			if edge.Type == "references" && edge.TargetID == "notes/long" {
				sawWikilink = true
			}
		}
	}

	if !sawWikilink {
		test.Errorf("expected references edge from a wikilink sub-unit → notes/long")
	}

	// Defensive: ensure no `tusk.toml` or `.tusk/` was accidentally
	// indexed (i.e., the walker honored ignore defaults).
	if _, statErr := os.Stat(filepath.Join(root, ".tusk", "index.db")); statErr != nil {
		test.Errorf("index db missing after run: %v", statErr)
	}
}
