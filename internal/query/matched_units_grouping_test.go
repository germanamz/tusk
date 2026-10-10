package query_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/query"
)

// seedLedger mirrors the #765 repro: an H1 with no direct leaves wrapping two
// H2 sections that each hold one paragraph. The query vector {1,0} favors the
// Balances paragraph (cosine 1) over the Entries one (cosine 0.6).
//
//	5  # Ledger
//	7  ## Entries
//	9  The ledger stores one row per entry.
//	11 ## Balances
//	13 Balances are derived from entries, never stored.
func seedLedger(test *testing.T, store *index.Index) {
	test.Helper()

	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{
		ID: "docs/ledger", Type: "note", Path: "docs/ledger.md", Title: "Ledger",
		PropertiesJSON: "{}", LastChecksum: "x",
	}); upsertErr != nil {
		test.Fatalf("file upsert: %v", upsertErr)
	}

	units := []struct {
		id, typ, parent, props, title, payload string
		ordinal, start, end                    int
		vector                                 []float32
	}{
		{id: "docs/ledger#S1", typ: "section", parent: "docs/ledger", props: `{"heading-level":1}`, title: "Ledger", payload: "Ledger\n...", ordinal: 0, start: 5, end: 13},
		{id: "docs/ledger#S1.1", typ: "section", parent: "docs/ledger#S1", props: `{"heading-level":2}`, title: "Entries", payload: "Entries\n...", ordinal: 1, start: 7, end: 9},
		{id: "docs/ledger#S1.1P1", typ: "paragraph", parent: "docs/ledger#S1.1", props: "{}", payload: "The ledger stores one row per entry.", ordinal: 2, start: 9, end: 9, vector: []float32{0.6, 0.8}},
		{id: "docs/ledger#S1.2", typ: "section", parent: "docs/ledger#S1", props: `{"heading-level":2}`, title: "Balances", payload: "Balances\n...", ordinal: 3, start: 11, end: 13},
		{id: "docs/ledger#S1.2P1", typ: "paragraph", parent: "docs/ledger#S1.2", props: "{}", payload: "Balances are derived from entries, never stored.", ordinal: 4, start: 13, end: 13, vector: []float32{1, 0}},
	}

	rows := make([]index.NodeRow, 0, len(units))

	for _, unit := range units {
		rows = append(rows, index.NodeRow{
			ID: unit.id, Type: unit.typ, Path: "docs/ledger.md", Title: unit.title,
			PropertiesJSON: unit.props, LastChecksum: "x",
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

func ledgerDeps(store *index.Index) query.Deps {
	return query.Deps{
		Database:   store.DB(),
		Manifest:   loadManifestWithSubUnits(nil),
		Nodes:      index.NewNodeRepo(store),
		Embedder:   stubEmbedder{vector: []float32{1, 0}},
		Embeddings: index.NewEmbeddingRepo(store),
	}
}

func runLedger(test *testing.T, request query.Request) *query.Result {
	test.Helper()

	store := openTestStore(test)
	seedLedger(test, store)

	if request.Filter == "" {
		request.Filter = "type=note"
	}

	result, runErr := query.Run(context.Background(), ledgerDeps(store), request)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return result
}

func unitIDs(units []query.MatchedUnit) []string {
	ids := make([]string, 0, len(units))

	for _, unit := range units {
		ids = append(ids, unit.ID)
	}

	return ids
}

// TestQueryRun_SemanticGroupsLeavesUnderInnermostSection pins #765 item 4:
// one finding is one row. Each scored leaf folds into its innermost section,
// which reports the leaf's score and snippet with its own heading and lines.
// The H1, which holds no leaf directly, does not repeat the finding.
func TestQueryRun_SemanticGroupsLeavesUnderInnermostSection(test *testing.T) {
	result := runLedger(test, query.Request{Semantic: "are balances stored"})

	file := result.Semantic.Ranked[0]

	if got := unitIDs(file.MatchedUnits); strings.Join(got, ",") != "docs/ledger#S1.2,docs/ledger#S1.1" {
		test.Fatalf("matched_units = %v, want [S1.2 S1.1]", got)
	}

	balances := file.MatchedUnits[0]

	assertScore(test, "Balances score", balances.Score, 1.0)
	assertScore(test, "Entries score", file.MatchedUnits[1].Score, 0.6)

	if balances.Type != "section" || balances.Heading != "Balances" || balances.HeadingLevel != 2 {
		test.Errorf("Balances row = %+v, want a level-2 section headed Balances", balances)
	}

	if balances.StartLine != 11 || balances.EndLine != 13 {
		test.Errorf("Balances lines = %d..%d, want 11..13", balances.StartLine, balances.EndLine)
	}

	if !strings.Contains(balances.Snippet, "never stored") {
		test.Errorf("Balances snippet = %q, want the best leaf's text", balances.Snippet)
	}

	if file.Score != balances.Score || file.Snippet != balances.Snippet {
		test.Errorf("file score/snippet = %v/%q, want the top row's %v/%q", file.Score, file.Snippet, balances.Score, balances.Snippet)
	}

	if file.UnitsTotal != 2 {
		test.Errorf("units_total = %d, want 2", file.UnitsTotal)
	}
}

// TestQueryRun_SemanticRootLeavesStayLeafRows: a leaf before the first
// heading has no section to fold into, so it stays a row of its own.
func TestQueryRun_SemanticRootLeavesStayLeafRows(test *testing.T) {
	store := openTestStore(test)
	seedAuthRFC(test, store)

	result, runErr := query.Run(context.Background(), query.Deps{
		Database:   store.DB(),
		Manifest:   loadManifestWithSubUnits(test),
		Nodes:      index.NewNodeRepo(store),
		Embedder:   stubEmbedder{vector: []float32{1, 0, 0}},
		Embeddings: index.NewEmbeddingRepo(store),
	}, query.Request{Filter: "type=note", Semantic: "OAuth PKCE"})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	got := unitIDs(result.Semantic.Ranked[0].MatchedUnits)

	if strings.Join(got, ",") != "notes/auth-rfc#sec_h2,notes/auth-rfc#sec_h3,notes/auth-rfc#para_top" {
		test.Errorf("matched_units = %v, want [sec_h2 sec_h3 para_top]", got)
	}
}

// TestQueryRun_SemanticBodyIsBestLeafChunk: include=body serves the passage
// that matched, not the top section's whole payload.
func TestQueryRun_SemanticBodyIsBestLeafChunk(test *testing.T) {
	result := runLedger(test, query.Request{Semantic: "balances", Include: []string{"body"}})

	if body := result.Semantic.Ranked[0].Body; body != "Balances are derived from entries, never stored." {
		test.Errorf("body = %q, want the best leaf's chunk", body)
	}
}

func TestQueryRun_MaxUnitsCapsSemanticUnits(test *testing.T) {
	cases := []struct {
		name         string
		maxUnits     int
		defaultUnits int
		want         int
	}{
		{name: "unset is unlimited", want: 2},
		{name: "explicit cap", maxUnits: 1, want: 1},
		{name: "default cap", defaultUnits: 1, want: 1},
		{name: "explicit beats default", maxUnits: 2, defaultUnits: 1, want: 2},
	}

	for _, testCase := range cases {
		result := runLedger(test, query.Request{
			Semantic:                "balances",
			MaxUnits:                testCase.maxUnits,
			SemanticDefaultMaxUnits: testCase.defaultUnits,
		})

		file := result.Semantic.Ranked[0]

		if len(file.MatchedUnits) != testCase.want {
			test.Errorf("%s: matched_units = %d, want %d", testCase.name, len(file.MatchedUnits), testCase.want)
		}

		if file.UnitsTotal != 2 {
			test.Errorf("%s: units_total = %d, want 2", testCase.name, file.UnitsTotal)
		}

		if file.MatchedUnits[0].ID != "docs/ledger#S1.2" {
			test.Errorf("%s: cap kept %s, want the best row first", testCase.name, file.MatchedUnits[0].ID)
		}
	}
}

// TestQueryRun_StructuralUnitsCarryHeadingAndLines: include=units lists the
// whole outline in document order with heading and lines. The semantic default
// cap does not apply (the outline was asked for); an explicit cap keeps the
// first N in document order.
func TestQueryRun_StructuralUnitsCarryHeadingAndLines(test *testing.T) {
	result := runLedger(test, query.Request{Include: []string{"units"}, SemanticDefaultMaxUnits: 1})

	row := result.Rows[0]

	if len(row.MatchedUnits) != 5 || row.UnitsTotal != 5 {
		test.Fatalf("matched_units = %d (units_total %d), want 5 (5)", len(row.MatchedUnits), row.UnitsTotal)
	}

	entries := row.MatchedUnits[1]

	if entries.Heading != "Entries" || entries.StartLine != 7 || entries.EndLine != 9 {
		test.Errorf("Entries row = %+v, want heading Entries at 7..9", entries)
	}

	if paragraph := row.MatchedUnits[2]; paragraph.Heading != "" || paragraph.StartLine != 9 {
		test.Errorf("paragraph row = %+v, want no heading, line 9", paragraph)
	}

	capped := runLedger(test, query.Request{Include: []string{"units"}, MaxUnits: 2})

	if got := unitIDs(capped.Rows[0].MatchedUnits); strings.Join(got, ",") != "docs/ledger#S1,docs/ledger#S1.1" {
		test.Errorf("capped outline = %v, want the first two in document order", got)
	}

	if capped.Rows[0].UnitsTotal != 5 {
		test.Errorf("capped units_total = %d, want 5", capped.Rows[0].UnitsTotal)
	}
}

func TestQueryRun_RejectsNegativeMaxUnits(test *testing.T) {
	store := openTestStore(test)
	seedLedger(test, store)

	_, runErr := query.Run(context.Background(), ledgerDeps(store), query.Request{Filter: "type=note", MaxUnits: -1})

	if runErr == nil || !strings.Contains(runErr.Error(), "max-units") {
		test.Errorf("Run error = %v, want a max-units error", runErr)
	}
}

func TestMatchedUnit_MarshalsHeadingAndLines(test *testing.T) {
	section, marshalErr := json.Marshal(query.MatchedUnit{
		ID: "docs/ledger#S1.2", Type: "section", Heading: "Balances", HeadingLevel: 2,
		StartLine: 11, EndLine: 13, Score: 0.8, HasScore: true,
	})

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	for _, want := range []string{`"heading":"Balances"`, `"lines":[11,13]`} {
		if !strings.Contains(string(section), want) {
			test.Errorf("section JSON %s lacks %s", section, want)
		}
	}

	leaf, leafErr := json.Marshal(query.MatchedUnit{ID: "docs/page.html#P1", Type: "paragraph"})

	if leafErr != nil {
		test.Fatalf("marshal leaf: %v", leafErr)
	}

	for _, absent := range []string{`"heading"`, `"lines"`} {
		if strings.Contains(string(leaf), absent) {
			test.Errorf("unpositioned leaf JSON %s carries %s", leaf, absent)
		}
	}
}
