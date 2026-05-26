package query_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
)

// stubEmbedder returns a single-fixed query vector for ranking tests.
type stubEmbedder struct {
	vector []float32
}

func (stub stubEmbedder) Embed(_ context.Context, _ []byte) ([]float32, error) {
	return stub.vector, nil
}

func (stub stubEmbedder) Model() string { return "stub" }
func (stub stubEmbedder) Dim() int      { return len(stub.vector) }

// openTestStore opens a fresh index db for a test.
func openTestStore(t *testing.T) *index.Index {
	t.Helper()

	store, openErr := index.Open(filepath.Join(t.TempDir(), "index.db"))

	if openErr != nil {
		t.Fatalf("open index: %v", openErr)
	}

	t.Cleanup(func() { store.Close() })

	return store
}

// seedAuthRFC populates the index with one note file row, two section
// sub-units (H2 and a nested H3), and three leaf sub-units (one
// paragraph under H2, one paragraph under H3, and one top-level
// paragraph). Each leaf gets a chunk vector aligned with the test's
// fixed query vector so cosine similarity scores are predictable.
func seedAuthRFC(t *testing.T, store *index.Index) {
	t.Helper()

	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	if err := nodes.Upsert(index.NodeRow{
		ID:             "notes/auth-rfc",
		Type:           "note",
		Path:           "notes/auth-rfc.md",
		Title:          "Auth RFC",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); err != nil {
		t.Fatalf("file upsert: %v", err)
	}

	subUnits := []struct {
		id      string
		typ     string
		ordinal int
		parent  string
		props   string
		payload string
		vector  []float32 // empty for sections
		isLeaf  bool
	}{
		{
			id: "notes/auth-rfc#sec_h2", typ: "section", ordinal: 0,
			parent:  "notes/auth-rfc",
			props:   `{"heading-level":2}`,
			payload: "Decision OAuth 2.1 with PKCE chosen for SSO migration",
		},
		{
			id: "notes/auth-rfc#para_h2", typ: "paragraph", ordinal: 1,
			parent:  "notes/auth-rfc#sec_h2",
			props:   "{}",
			payload: "Users with SSO accounts hit the password reset flow when OAuth PKCE fails",
			vector:  []float32{0.9, 0.1, 0.0},
			isLeaf:  true,
		},
		{
			id: "notes/auth-rfc#sec_h3", typ: "section", ordinal: 2,
			parent:  "notes/auth-rfc#sec_h2",
			props:   `{"heading-level":3}`,
			payload: "PKCE implementation: code-verifier generation",
		},
		{
			id: "notes/auth-rfc#para_h3", typ: "paragraph", ordinal: 3,
			parent:  "notes/auth-rfc#sec_h3",
			props:   "{}",
			payload: "code-verifier generation uses cryptographic randomness for PKCE",
			vector:  []float32{0.7, 0.2, 0.0},
			isLeaf:  true,
		},
		{
			id: "notes/auth-rfc#para_top", typ: "paragraph", ordinal: 4,
			parent:  "notes/auth-rfc",
			props:   "{}",
			payload: "Background notes on identity providers and SAML",
			vector:  []float32{0.0, 1.0, 0.0},
			isLeaf:  true,
		},
	}

	subUnitRows := make([]index.NodeRow, 0, len(subUnits))

	for _, unit := range subUnits {
		subUnitRows = append(subUnitRows, index.NodeRow{
			ID:             unit.id,
			Type:           unit.typ,
			Path:           "notes/auth-rfc.md",
			Title:          "",
			PropertiesJSON: unit.props,
			LastChecksum:   "x",
			ParentID:       sql.NullString{String: unit.parent, Valid: true},
			Ordinal:        sql.NullInt64{Int64: int64(unit.ordinal), Valid: true},
			EmbedPayload:   sql.NullString{String: unit.payload, Valid: true},
		})
	}

	if err := nodes.BulkUpsert(subUnitRows); err != nil {
		t.Fatalf("sub-unit bulk upsert: %v", err)
	}

	for _, unit := range subUnits {
		if !unit.isLeaf {
			continue
		}

		if err := embeddings.Upsert(index.EmbeddingRow{
			NodeID:      unit.id,
			ChunkIdx:    0,
			Model:       "stub",
			ContentHash: "h_" + unit.id,
			Vector:      unit.vector,
			Dim:         3,
			Body:        unit.payload,
		}); err != nil {
			t.Fatalf("embedding upsert %s: %v", unit.id, err)
		}
	}
}

func loadManifestWithSubUnits(_ *testing.T) *manifest.Manifest {
	loaded := &manifest.Manifest{}
	manifest.MergeBuiltinPacks(loaded)

	return loaded
}

func TestQueryRun_StructuralIncludeUnitsAttachesSubUnits(test *testing.T) {
	store := openTestStore(test)
	seedAuthRFC(test, store)

	deps := query.Deps{
		Database: store.DB(),
		Manifest: loadManifestWithSubUnits(test),
		Nodes:    index.NewNodeRepo(store),
	}

	result, runErr := query.Run(context.Background(), deps, query.Request{
		Filter:  "type=note",
		Include: []string{"units"},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(result.Rows) != 1 {
		test.Fatalf("rows = %d, want 1", len(result.Rows))
	}

	row := result.Rows[0]

	if row.ID != "notes/auth-rfc" {
		test.Errorf("row id = %q, want notes/auth-rfc", row.ID)
	}

	if len(row.MatchedUnits) != 5 {
		test.Fatalf("matched_units = %d, want 5", len(row.MatchedUnits))
	}

	// Structural include=units must not stamp scores.
	for _, unit := range row.MatchedUnits {
		if unit.HasScore {
			test.Errorf("unit %s carries HasScore=true on structural path", unit.ID)
		}
	}

	// Sections expose heading_level; leaves do not.
	for _, unit := range row.MatchedUnits {
		switch unit.ID {
		case "notes/auth-rfc#sec_h2":
			if unit.HeadingLevel != 2 {
				test.Errorf("sec_h2 heading_level = %d, want 2", unit.HeadingLevel)
			}
		case "notes/auth-rfc#sec_h3":
			if unit.HeadingLevel != 3 {
				test.Errorf("sec_h3 heading_level = %d, want 3", unit.HeadingLevel)
			}
		default:
			if unit.HeadingLevel != 0 {
				test.Errorf("%s heading_level = %d, want 0", unit.ID, unit.HeadingLevel)
			}
		}
	}

	// JSON marshalling drops score for structural path and keeps
	// heading_level only on sections.
	marshalled, marshalErr := json.Marshal(row.MatchedUnits[0])

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	if strings.Contains(string(marshalled), `"score"`) {
		test.Errorf("structural unit JSON includes score: %s", marshalled)
	}
}

func TestQueryRun_DirectSubUnitQueryReturnsRowsWithParentID(test *testing.T) {
	store := openTestStore(test)
	seedAuthRFC(test, store)

	deps := query.Deps{
		Database: store.DB(),
		Manifest: loadManifestWithSubUnits(test),
		Nodes:    index.NewNodeRepo(store),
	}

	result, runErr := query.Run(context.Background(), deps, query.Request{
		Filter: "type=section",
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(result.Rows) != 2 {
		test.Fatalf("rows = %d, want 2 sections", len(result.Rows))
	}

	for _, row := range result.Rows {
		if row.Type != "section" {
			test.Errorf("row %s type = %q, want section", row.ID, row.Type)
		}

		if row.ParentID == "" {
			test.Errorf("row %s missing parent_id", row.ID)
		}

		if row.MatchedUnits != nil {
			test.Errorf("direct sub-unit row %s carries matched_units (wrap)", row.ID)
		}
	}
}

func TestQueryRun_SemanticSubUnitsGroupsByParent(test *testing.T) {
	store := openTestStore(test)
	seedAuthRFC(test, store)

	deps := query.Deps{
		Database:   store.DB(),
		Manifest:   loadManifestWithSubUnits(test),
		Nodes:      index.NewNodeRepo(store),
		Embedder:   stubEmbedder{vector: []float32{1, 0, 0}},
		Embeddings: index.NewEmbeddingRepo(store),
	}

	result, runErr := query.Run(context.Background(), deps, query.Request{
		Filter:   "type=note",
		Semantic: "OAuth PKCE",
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Semantic == nil {
		test.Fatalf("expected Semantic result")
	}

	if len(result.Semantic.Ranked) != 1 {
		test.Fatalf("ranked = %d, want 1 file (grouped)", len(result.Semantic.Ranked))
	}

	file := result.Semantic.Ranked[0]

	if file.ID != "notes/auth-rfc" {
		test.Errorf("file id = %q, want notes/auth-rfc", file.ID)
	}

	if len(file.MatchedUnits) == 0 {
		test.Fatalf("matched_units is empty")
	}

	// Leaves: para_h2 (0.9), para_h3 (~0.7-ish; the embed payload
	// vector is {0.7,0.2,0}), para_top (0). Sections: H2 score =
	// 0.85 * 0.9 = 0.765; H3 score = 0.70 * 0.7 ≈ 0.49.
	scoreByID := map[string]float64{}

	for _, unit := range file.MatchedUnits {
		scoreByID[unit.ID] = unit.Score

		if !unit.HasScore {
			test.Errorf("semantic unit %s missing HasScore", unit.ID)
		}
	}

	if scoreByID["notes/auth-rfc#para_h2"] < 0.85 {
		test.Errorf("para_h2 score = %f, want > 0.85", scoreByID["notes/auth-rfc#para_h2"])
	}

	// Section H2 score should be 0.85 × para_h2 score.
	expectedH2 := 0.85 * scoreByID["notes/auth-rfc#para_h2"]
	actualH2 := scoreByID["notes/auth-rfc#sec_h2"]

	if diff := actualH2 - expectedH2; diff > 1e-6 || diff < -1e-6 {
		test.Errorf("section H2 score = %f, want %f (0.85 * leaf)", actualH2, expectedH2)
	}

	// Section H3 score should be 0.70 × para_h3 score. Locks in nested-
	// section aggregation: H3 contains only para_h3, no sibling leaves.
	expectedH3 := 0.70 * scoreByID["notes/auth-rfc#para_h3"]
	actualH3 := scoreByID["notes/auth-rfc#sec_h3"]

	if diff := actualH3 - expectedH3; diff > 1e-6 || diff < -1e-6 {
		test.Errorf("section H3 score = %f, want %f (0.70 * leaf)", actualH3, expectedH3)
	}

	// File-level score must equal the max across all units (likely
	// para_h2 with the highest raw score).
	maxScore := 0.0

	for _, score := range scoreByID {
		if score > maxScore {
			maxScore = score
		}
	}

	if file.Score != maxScore {
		test.Errorf("file score = %f, want max %f", file.Score, maxScore)
	}

	// MatchedUnits must be sorted descending by score.
	for idx := 1; idx < len(file.MatchedUnits); idx++ {
		if file.MatchedUnits[idx-1].Score < file.MatchedUnits[idx].Score {
			test.Errorf("matched_units not score-descending at %d", idx)
		}
	}
}

func TestQueryRun_SubUnitsDisabledKeepsLegacyShape(test *testing.T) {
	store := openTestStore(test)

	// Seed a single file with a file-level embedding (no sub-units).
	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	if err := nodes.Upsert(index.NodeRow{
		ID:             "notes/legacy",
		Type:           "note",
		Path:           "notes/legacy.md",
		Title:          "Legacy note",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); err != nil {
		test.Fatalf("upsert: %v", err)
	}

	if err := embeddings.Upsert(index.EmbeddingRow{
		NodeID: "notes/legacy", ChunkIdx: 0, Model: "stub",
		ContentHash: "h", Vector: []float32{1, 0, 0}, Dim: 3,
		Body: "legacy body content for snippet",
	}); err != nil {
		test.Fatalf("embed upsert: %v", err)
	}

	// Manifest with sub-units explicitly disabled.
	loaded := loadManifestWithSubUnitsDisabled(test)
	manifest.MergeBuiltinPacks(loaded)

	deps := query.Deps{
		Database:   store.DB(),
		Manifest:   loaded,
		Nodes:      index.NewNodeRepo(store),
		Embedder:   stubEmbedder{vector: []float32{1, 0, 0}},
		Embeddings: index.NewEmbeddingRepo(store),
	}

	// Semantic: should rank file-level row, not group anything.
	result, runErr := query.Run(context.Background(), deps, query.Request{
		Filter:   "type=note",
		Semantic: "anything",
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Semantic == nil {
		test.Fatalf("expected Semantic result")
	}

	if len(result.Semantic.Ranked) != 1 {
		test.Fatalf("ranked = %d, want 1", len(result.Semantic.Ranked))
	}

	if len(result.Semantic.Ranked[0].MatchedUnits) != 0 {
		test.Errorf("legacy path must not emit matched_units")
	}

	// Structural include=units silently no-ops (no sub-units in db).
	structural, structErr := query.Run(context.Background(), deps, query.Request{
		Filter:  "type=note",
		Include: []string{"units"},
	})

	if structErr != nil {
		test.Fatalf("Run structural: %v", structErr)
	}

	if len(structural.Rows) != 1 {
		test.Fatalf("rows = %d, want 1", len(structural.Rows))
	}

	if structural.Rows[0].MatchedUnits != nil {
		test.Errorf("structural include=units must be a no-op when sub-units disabled, got %d", len(structural.Rows[0].MatchedUnits))
	}
}

// loadManifestWithSubUnitsDisabled returns a Manifest decoded from an
// inline TOML body that explicitly sets `sub-units = false`. Using the
// real loader (rather than constructing a literal) is the only way to
// populate the toml.MetaData that SubUnitsEnabled inspects.
func loadManifestWithSubUnitsDisabled(test *testing.T) *manifest.Manifest {
	test.Helper()

	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")

	body := "[workspace]\nname = \"test\"\nsub-units = false\n"

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("load manifest: %v", loadErr)
	}

	return loaded
}
