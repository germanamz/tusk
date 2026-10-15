package query_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/query"
)

// schemesSource is the #814 repro in miniature: the best leaf is one bullet
// of a list, and the answer needs its sibling too.
//
//	9  ## Values
//	11 - `lf`: the default; only LF ends a line.
//	12 - `universal`: adds a lone CR.
//	13 - `unicode`: adds the Unicode breaks.
//	15 ## Notes
//	17 Unrelated notes paragraph.
const schemesSource = "---\ntitle: Schemes\n---\n\n# Schemes\n\nIntro paragraph about schemes.\n\n## Values\n\n" +
	"- `lf`: the default; only LF ends a line.\n- `universal`: adds a lone CR.\n- `unicode`: adds the Unicode breaks.\n\n" +
	"## Notes\n\nUnrelated notes paragraph.\n"

// glossarySource is a second, weaker file whose only passage sits before
// the first heading.
const glossarySource = "A carriage return is a control character.\n\nIt moves the cursor to the line start.\n"

type passageUnit struct {
	id, typ, parent, title, payload string
	ordinal, start, end             int
	vector                          []float32
}

// seedPassageFile writes source to root/fileID.md and indexes the file row
// (its checksum taken from checksumOf) and the given sub-units.
func seedPassageFile(test *testing.T, store *index.Index, root, fileID, source, checksumOf string, units []passageUnit) {
	test.Helper()

	path := fileID + ".md"
	fullPath := filepath.Join(root, path)

	if mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); mkdirErr != nil {
		test.Fatalf("mkdir: %v", mkdirErr)
	}

	if writeErr := os.WriteFile(fullPath, []byte(source), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", path, writeErr)
	}

	checksum := sha256.Sum256([]byte(checksumOf))
	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{
		ID: fileID, Type: "note", Path: path, Title: fileID,
		PropertiesJSON: "{}", LastChecksum: hex.EncodeToString(checksum[:]),
	}); upsertErr != nil {
		test.Fatalf("file upsert: %v", upsertErr)
	}

	rows := make([]index.NodeRow, 0, len(units))

	for _, unit := range units {
		rows = append(rows, index.NodeRow{
			ID: unit.id, Type: unit.typ, Path: path, Title: unit.title,
			PropertiesJSON: "{}", LastChecksum: "x",
			ParentID:     sql.NullString{String: unit.parent, Valid: true},
			Ordinal:      sql.NullInt64{Int64: int64(unit.ordinal), Valid: true},
			EmbedPayload: sql.NullString{String: unit.payload, Valid: true},
			StartLine:    sql.NullInt64{Int64: int64(unit.start), Valid: true},
			EndLine:      sql.NullInt64{Int64: int64(unit.end), Valid: true},
		})
	}

	if upsertErr := nodes.BulkUpsert(rows, "markdown"); upsertErr != nil {
		test.Fatalf("sub-unit upsert: %v", upsertErr)
	}

	for _, unit := range units {
		if unit.vector == nil {
			continue
		}

		if upsertErr := embeddings.Upsert(index.EmbeddingRow{
			NodeID: unit.id, Model: "stub", ContentHash: "h_" + unit.id,
			Vector: unit.vector, Dim: len(unit.vector), Body: unit.payload,
		}); upsertErr != nil {
			test.Fatalf("embedding upsert %s: %v", unit.id, upsertErr)
		}
	}
}

// runPassages seeds both files and runs a semantic query rooted at their
// directory. staleSchemes indexes notes/schemes under a checksum that no
// longer matches its file, as after an edit the index has not caught up with.
func runPassages(test *testing.T, include []string, staleSchemes bool) []query.ScoredRow {
	test.Helper()

	store := openTestStore(test)
	root := test.TempDir()

	schemesChecksum := schemesSource

	if staleSchemes {
		schemesChecksum = "an older version of the file"
	}

	seedPassageFile(test, store, root, "notes/schemes", schemesSource, schemesChecksum, []passageUnit{
		{id: "notes/schemes#S1", typ: "section", parent: "notes/schemes", title: "Schemes", payload: "Schemes", ordinal: 0, start: 5, end: 17},
		{id: "notes/schemes#S1P1", typ: "paragraph", parent: "notes/schemes#S1", payload: "Intro paragraph about schemes.", ordinal: 1, start: 7, end: 7, vector: []float32{0.5, 0.866}},
		{id: "notes/schemes#S1.1", typ: "section", parent: "notes/schemes#S1", title: "Values", payload: "Values", ordinal: 2, start: 9, end: 13},
		{id: "notes/schemes#S1.1L1", typ: "list-item", parent: "notes/schemes#S1.1", payload: "lf: the default; only LF ends a line.", ordinal: 3, start: 11, end: 11, vector: []float32{0.6, 0.8}},
		{id: "notes/schemes#S1.1L2", typ: "list-item", parent: "notes/schemes#S1.1", payload: "universal: adds a lone CR.", ordinal: 4, start: 12, end: 12, vector: []float32{1, 0}},
		{id: "notes/schemes#S1.1L3", typ: "list-item", parent: "notes/schemes#S1.1", payload: "unicode: adds the Unicode breaks.", ordinal: 5, start: 13, end: 13},
		{id: "notes/schemes#S1.2", typ: "section", parent: "notes/schemes#S1", title: "Notes", payload: "Notes", ordinal: 6, start: 15, end: 17},
		{id: "notes/schemes#S1.2P1", typ: "paragraph", parent: "notes/schemes#S1.2", payload: "Unrelated notes paragraph.", ordinal: 7, start: 17, end: 17},
	})

	seedPassageFile(test, store, root, "notes/glossary", glossarySource, glossarySource, []passageUnit{
		{id: "notes/glossary#P1", typ: "paragraph", parent: "notes/glossary", payload: "A carriage return is a control character.", ordinal: 0, start: 1, end: 1, vector: []float32{0.8, 0.6}},
		{id: "notes/glossary#P2", typ: "paragraph", parent: "notes/glossary", payload: "It moves the cursor to the line start.", ordinal: 1, start: 3, end: 3},
	})

	result, runErr := query.Run(context.Background(), query.Deps{
		Database:   store.DB(),
		Manifest:   loadManifestWithSubUnits(test),
		Nodes:      index.NewNodeRepo(store),
		Embedder:   stubEmbedder{vector: []float32{1, 0}},
		Embeddings: index.NewEmbeddingRepo(store),
	}, query.Request{Filter: "type=note", Semantic: "lone carriage return", Include: include, WorkspaceRoot: root})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if ids := scoredIDs(result.Semantic.Ranked); ids != "notes/schemes,notes/glossary" {
		test.Fatalf("ranked = %s, want notes/schemes,notes/glossary", ids)
	}

	return result.Semantic.Ranked
}

func scoredIDs(rows []query.ScoredRow) string {
	ids := make([]string, 0, len(rows))

	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	return strings.Join(ids, ",")
}

// TestQueryRun_TopRowCarriesPassageText pins #814: the top row's best unit
// carries its section's own text, read from the file, so the sibling bullet
// the best leaf alone would miss arrives with the query. Lower rows keep
// their snippets.
func TestQueryRun_TopRowCarriesPassageText(test *testing.T) {
	ranked := runPassages(test, nil, false)
	values := ranked[0].MatchedUnits[0]

	want := "- `lf`: the default; only LF ends a line.\n- `universal`: adds a lone CR.\n- `unicode`: adds the Unicode breaks."

	if values.ID != "notes/schemes#S1.1" || values.Text != want {
		test.Errorf("top unit %s text = %q, want the Values section's own text %q", values.ID, values.Text, want)
	}

	if values.TextStartLine != 11 || values.TextEndLine != 13 {
		test.Errorf("text lines = %d..%d, want 11..13", values.TextStartLine, values.TextEndLine)
	}

	if text := ranked[1].MatchedUnits[0].Text; text != "" {
		test.Errorf("second row text = %q, want none without include=body", text)
	}

	unit := jsonObject(test, values)

	if got := sortedKeys(unit); got != "heading,id,lines,score,text,text_lines" {
		test.Errorf("unit keys = %s, want text and text_lines in place of the snippet", got)
	}
}

// TestQueryRun_IncludeBodyGivesEveryRowPassageText: include=body extends the
// passage text to every row, and the row-level body (the best leaf's chunk,
// which the text contains) is left out. A root leaf's passage is the leaves
// before the first heading.
func TestQueryRun_IncludeBodyGivesEveryRowPassageText(test *testing.T) {
	ranked := runPassages(test, []string{"body"}, false)

	glossary := ranked[1].MatchedUnits[0]

	if glossary.Text != "A carriage return is a control character.\n\nIt moves the cursor to the line start." {
		test.Errorf("glossary text = %q, want both root paragraphs", glossary.Text)
	}

	for _, row := range ranked {
		if row.Body != "" {
			test.Errorf("%s body = %q, want none beside the unit text", row.ID, row.Body)
		}
	}
}

// TestQueryRun_StaleFileKeepsSnippet: a file edited since it was indexed may
// have moved its lines, so its unit carries no text, and include=body falls
// back to the best leaf's chunk.
func TestQueryRun_StaleFileKeepsSnippet(test *testing.T) {
	ranked := runPassages(test, []string{"body"}, true)
	values := ranked[0].MatchedUnits[0]

	if values.Text != "" {
		test.Errorf("stale file text = %q, want none", values.Text)
	}

	if ranked[0].Body != "universal: adds a lone CR." {
		test.Errorf("stale file body = %q, want the best leaf's chunk", ranked[0].Body)
	}

	if ranked[1].MatchedUnits[0].Text == "" {
		test.Error("fresh second file has no text, want its passage")
	}
}
