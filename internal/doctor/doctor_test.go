package doctor_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

func TestRun_NoIssuesOnFreshIndex(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(report.Issues) != 0 {
		test.Errorf("expected 0 issues, got %d: %+v", len(report.Issues), report.Issues)
	}
}

func TestRun_FlagsDanglingEdges(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	nodeRepo.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", Title: "A", PropertiesJSON: "{}", LastChecksum: "x"})
	edgeRepo.UpsertAll("tickets/a", "tickets/a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/missing", SourcePath: "tickets/a.md"},
	})

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      nodeRepo,
		Edges:      edgeRepo,
		EmbedQueue: index.NewEmbedQueueRepo(store),
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(report.Issues) == 0 {
		test.Fatalf("expected dangling edge to be flagged")
	}

	if report.Issues[0].Kind != doctor.IssueDanglingEdge {
		test.Errorf("kind = %q, want %q", report.Issues[0].Kind, doctor.IssueDanglingEdge)
	}
}

func TestRun_ReportsQueueDepth(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	queueRepo.Enqueue("notes/x")
	queueRepo.Enqueue("notes/y")

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: queueRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedQueueDepth != 2 {
		test.Errorf("EmbedQueueDepth = %d, want 2", report.EmbedQueueDepth)
	}
}

func TestRun_SurfacesWorkflowViolation(test *testing.T) {
	store, closer := newTempIndex(test)
	defer closer()

	driftRepo := index.NewWorkflowDriftRepo(store)

	if appendErr := driftRepo.Append(index.WorkflowDriftRow{
		NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "blocked", Property: "status", ObservedAt: 100,
	}); appendErr != nil {
		test.Fatalf("Append: %v", appendErr)
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:         index.NewNodeRepo(store),
		WorkflowDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	var found bool

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueWorkflowViolation && issue.NodeID == "tickets/foo" {
			found = true

			if !strings.Contains(issue.Message, "blocked") {
				test.Errorf("Issue.Message = %q, want mention of 'blocked'", issue.Message)
			}
		}
	}

	if !found {
		test.Errorf("workflow-violation Issue not found in %+v", report.Issues)
	}
}

func TestRun_SurfacesPropertyDrift(test *testing.T) {
	store, closer := newTempIndex(test)
	defer closer()

	driftRepo := index.NewPropertyDriftRepo(store)

	rows := []index.PropertyDriftRow{
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "undeclared-property", Property: "assignee", Details: "not declared on type \"ticket\"", ObservedAt: 100},
		{NodeID: "tickets/bar", NodeType: "ticket", Kind: "type-mismatch", Property: "priority", Details: "value \"high\" is not an integer", ObservedAt: 200},
		{NodeID: "tickets/baz", NodeType: "ticket", Kind: "required-missing", Property: "summary", Details: "required", ObservedAt: 300},
		{NodeID: "tickets/qux", NodeType: "ticket", Kind: "enum-violation", Property: "stage", Details: "value \"shipping\" not in [pending, active, completed]", ObservedAt: 400},
	}

	for _, row := range rows {
		if appendErr := driftRepo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:         index.NewNodeRepo(store),
		PropertyDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	wantKinds := []string{
		doctor.IssueUndeclaredProperty,
		doctor.IssueTypeMismatch,
		doctor.IssueRequiredMissing,
		doctor.IssueEnumViolation,
	}

	for _, want := range wantKinds {
		var found bool

		for _, issue := range report.Issues {
			if issue.Kind == want {
				found = true

				if !strings.Contains(issue.Message, "node-types") {
					test.Errorf("Issue %s: message %q missing 'node-types' prefix", want, issue.Message)
				}
			}
		}

		if !found {
			test.Errorf("kind %q not surfaced", want)
		}
	}
}

func TestRun_SurfacesRefDangling(test *testing.T) {
	idx, _ := index.Open(filepath.Join(test.TempDir(), "idx.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)

	if appendErr := driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/auth",
		NodeType: "ticket",
		Kind:     doctor.IssueRefDangling,
		Property: "assignee",
		Details:  `{"value":"alice","to":"person"}`,
	}); appendErr != nil {
		test.Fatalf("append: %v", appendErr)
	}

	report, runErr := doctor.Run(doctor.Config{PropertyDrift: driftRepo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(report.Issues) != 1 || report.Issues[0].Kind != doctor.IssueRefDangling {
		test.Fatalf("Issues = %+v", report.Issues)
	}

	if !strings.Contains(report.Issues[0].Message, "alice") || !strings.Contains(report.Issues[0].Message, "person") {
		test.Errorf("Message = %q", report.Issues[0].Message)
	}
}

func TestRun_SurfacesRefAmbiguous(test *testing.T) {
	idx, _ := index.Open(filepath.Join(test.TempDir(), "idx.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)

	if appendErr := driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/auth",
		NodeType: "ticket",
		Kind:     doctor.IssueRefAmbiguous,
		Property: "assignee",
		Details:  `{"value":"alice","to":"person","candidates":["people/alice-1","people/alice-2"]}`,
	}); appendErr != nil {
		test.Fatalf("append: %v", appendErr)
	}

	report, _ := doctor.Run(doctor.Config{PropertyDrift: driftRepo})

	if !strings.Contains(report.Issues[0].Message, "people/alice-1") {
		test.Errorf("Message = %q", report.Issues[0].Message)
	}
}

func TestRun_SurfacesRefTypeMismatch(test *testing.T) {
	idx, _ := index.Open(filepath.Join(test.TempDir(), "idx.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)

	driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/auth",
		NodeType: "ticket",
		Kind:     doctor.IssueRefTypeMismatch,
		Property: "assignee",
		Details:  `{"value":"[[people/bob]]","to":"person","actual_type":"user"}`,
	})

	report, _ := doctor.Run(doctor.Config{PropertyDrift: driftRepo})

	if !strings.Contains(report.Issues[0].Message, "user") || !strings.Contains(report.Issues[0].Message, "person") {
		test.Errorf("Message = %q", report.Issues[0].Message)
	}
}

func TestRun_SurfacesRefCycle(test *testing.T) {
	idx, _ := index.Open(filepath.Join(test.TempDir(), "idx.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)

	driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/c",
		NodeType: "ticket",
		Kind:     doctor.IssueRefCycle,
		Property: "parent",
		Details:  `{"path":["tickets/a","tickets/b","tickets/c","tickets/a"]}`,
	})

	report, _ := doctor.Run(doctor.Config{PropertyDrift: driftRepo})

	if !strings.Contains(report.Issues[0].Message, "cycle") {
		test.Errorf("Message = %q", report.Issues[0].Message)
	}
}

func newTempIndex(test *testing.T) (*index.Index, func()) {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store, func() { store.Close() }
}

func TestRun_EmbedStatsAndIssues(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	embedQueue := index.NewEmbedQueueRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	// nodeA: 1 chunk @ small body
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "a", Type: "note", Title: "A", Path: "a.md", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert a: %v", upsertErr)
	}

	if upsertErr := embeddings.Upsert(index.EmbeddingRow{
		NodeID: "a", ChunkIdx: 0, Model: "m", ContentHash: "h",
		Vector: []float32{0.1}, Dim: 1, Body: "short body",
	}); upsertErr != nil {
		test.Fatalf("upsert embed a: %v", upsertErr)
	}

	// nodeB: 1 chunk @ near-MaxBytes body
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "b", Type: "note", Title: "B", Path: "b.md", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert b: %v", upsertErr)
	}

	if upsertErr := embeddings.Upsert(index.EmbeddingRow{
		NodeID: "b", ChunkIdx: 0, Model: "m", ContentHash: "h",
		Vector: []float32{0.1}, Dim: 1, Body: strings.Repeat("x", 3800),
	}); upsertErr != nil {
		test.Fatalf("upsert embed b: %v", upsertErr)
	}

	// nodeC: indexed, no embeddings, NOT pending → should flag.
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "c", Type: "note", Title: "C", Path: "c.md", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert c: %v", upsertErr)
	}

	// nodeD: indexed, no embeddings, IS pending → should NOT flag.
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "d", Type: "note", Title: "D", Path: "d.md", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert d: %v", upsertErr)
	}

	if enqErr := embedQueue.Enqueue("d"); enqErr != nil {
		test.Fatalf("enqueue d: %v", enqErr)
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      nodes,
		Edges:      edges,
		EmbedQueue: embedQueue,
		Embeddings: embeddings,
		Manifest:   &manifest.Manifest{Embeddings: manifest.EmbeddingsSection{Provider: "ollama"}},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedStats == nil {
		test.Fatal("EmbedStats is nil")
	}

	if report.EmbedStats.TotalNodes != 2 || report.EmbedStats.TotalChunks != 2 {
		test.Errorf("EmbedStats counts: %+v", report.EmbedStats)
	}

	var sawLarge, sawNoChunks bool

	for _, issue := range report.Issues {
		switch issue.Kind {
		case doctor.IssueEmbedLargeChunk:
			if issue.NodeID == "b" {
				sawLarge = true
			}
		case doctor.IssueEmbedNoChunks:
			if issue.NodeID == "c" {
				sawNoChunks = true
			}

			if issue.NodeID == "d" {
				test.Errorf("pending node d reported as no-chunks")
			}
		}
	}

	if !sawLarge {
		test.Errorf("missing embed-large-chunk for node b: %+v", report.Issues)
	}

	if !sawNoChunks {
		test.Errorf("missing embed-no-chunks for node c: %+v", report.Issues)
	}
}

func TestRun_EmbedStatsNilWithoutEmbeddingsConfig(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedStats != nil {
		test.Errorf("EmbedStats = %+v, want nil", report.EmbedStats)
	}
}

// TestRun_SurfacesSubUnitConflicts confirms that reserved-name
// conflicts captured by manifest.MergeBuiltinPacks flow through doctor
// into both the typed Report.SubUnitConflicts list and the legacy
// per-issue rendering (IssueSubUnitReserved).
func TestRun_SurfacesSubUnitConflicts(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	loaded := &manifest.Manifest{
		SubUnitConflicts: []manifest.SubUnitConflict{
			{Kind: "node-type", Name: "section", Message: "node-types.section: reserved"},
			{Kind: "edge-type", Name: "contains", Message: "edge-types.contains: reserved"},
			{Kind: "property", OwnerType: "section", Name: "heading-level",
				Message: "node-types.section.heading-level: reserved"},
		},
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Manifest:   loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(report.SubUnitConflicts) != 3 {
		test.Errorf("SubUnitConflicts len = %d, want 3", len(report.SubUnitConflicts))
	}

	reservedIssues := 0

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueSubUnitReserved {
			reservedIssues++
		}
	}

	if reservedIssues != 3 {
		test.Errorf("IssueSubUnitReserved count = %d, want 3", reservedIssues)
	}

	// Property conflicts encode their owner in NodeID for the legacy
	// issue stream so callers can tell `section.heading-level` apart
	// from a top-level `heading-level` property collision.
	sawPropertyOwner := false

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueSubUnitReserved && strings.Contains(issue.NodeID, "section.heading-level") {
			sawPropertyOwner = true
		}
	}

	if !sawPropertyOwner {
		test.Errorf("expected property conflict to carry owner.property in NodeID; got %+v", report.Issues)
	}
}

// loadSubUnitsDisabledManifest writes a minimal tusk.toml with
// `[workspace] sub-units = false` and returns the loaded *Manifest.
// SubUnitsEnabled requires the toml.MetaData captured at decode time,
// so test fixtures must load through manifest.Load rather than build a
// literal Manifest in memory.
func loadSubUnitsDisabledManifest(test *testing.T) *manifest.Manifest {
	test.Helper()

	path := filepath.Join(test.TempDir(), "tusk.toml")

	body := `
[workspace]
name = "x"
sub-units = false
`

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	if loaded.SubUnitsEnabled() {
		test.Fatalf("loaded manifest still has SubUnitsEnabled() = true; check fixture")
	}

	return loaded
}

// TestRun_SubUnitPaneNilOnDisabledCleanIndex exercises the back-compat
// path: the manifest opts out of sub-units AND the index has no
// sub-unit rows. The pane should be entirely absent so existing doctor
// renderers see no new output.
func TestRun_SubUnitPaneNilOnDisabledCleanIndex(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	loaded := loadSubUnitsDisabledManifest(test)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Manifest:   loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.SubUnitPane != nil {
		test.Errorf("SubUnitPane = %+v, want nil for opt-out + empty index", report.SubUnitPane)
	}
}

// TestRun_SubUnitPaneCountsKindsAndCollisions seeds a handful of
// sub-unit rows of varied kinds (plus one with the disambiguation
// suffix and one oversize embed payload) and confirms every field in
// the pane lights up correctly.
func TestRun_SubUnitPaneCountsKindsAndCollisions(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodes := index.NewNodeRepo(store)
	queue := index.NewEmbedQueueRepo(store)

	// Parent file row.
	if upsertErr := nodes.Upsert(index.NodeRow{
		ID: "notes/n", Type: "note", Path: "notes/n.md", Title: "N",
		PropertiesJSON: "{}", LastChecksum: "x",
	}); upsertErr != nil {
		test.Fatalf("upsert parent: %v", upsertErr)
	}

	parentID := sql.NullString{String: "notes/n", Valid: true}

	// Variety of sub-unit kinds.
	subRows := []index.NodeRow{
		{ID: "notes/n#aaaa", Type: "section", Path: "notes/n.md", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: parentID, Ordinal: sql.NullInt64{Int64: 0, Valid: true},
			EmbedPayload: sql.NullString{String: "tiny", Valid: true}},
		{ID: "notes/n#bbbb", Type: "paragraph", Path: "notes/n.md", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: parentID, Ordinal: sql.NullInt64{Int64: 1, Valid: true},
			EmbedPayload: sql.NullString{String: "tiny", Valid: true}},
		{ID: "notes/n#cccc", Type: "list-item", Path: "notes/n.md", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: parentID, Ordinal: sql.NullInt64{Int64: 2, Valid: true},
			EmbedPayload: sql.NullString{String: "tiny", Valid: true}},
		// Disambiguation-suffix row: id ends in `-1`. Should be counted
		// as a hash collision.
		{ID: "notes/n#dddd-1", Type: "paragraph", Path: "notes/n.md", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: parentID, Ordinal: sql.NullInt64{Int64: 3, Valid: true},
			EmbedPayload: sql.NullString{String: "tiny", Valid: true}},
		// Oversize embed payload (> embed.DefaultMaxBytes).
		{ID: "notes/n#eeee", Type: "code-block", Path: "notes/n.md", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: parentID, Ordinal: sql.NullInt64{Int64: 4, Valid: true},
			EmbedPayload: sql.NullString{String: strings.Repeat("y", embed.DefaultMaxBytes+1), Valid: true}},
	}

	for _, row := range subRows {
		if upsertErr := nodes.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert sub-unit %s: %v", row.ID, upsertErr)
		}
	}

	// File-level enqueue + a sub-unit enqueue so the bucket counters
	// each light up.
	if enqErr := queue.Enqueue("notes/n"); enqErr != nil {
		test.Fatalf("enqueue file: %v", enqErr)
	}

	if enqErr := queue.Enqueue("notes/n#cccc"); enqErr != nil {
		test.Fatalf("enqueue sub-unit: %v", enqErr)
	}

	// File ID that ends in `-2` to assert the collision GLOB doesn't
	// false-positive. We don't enqueue this one; just create it.
	if upsertErr := nodes.Upsert(index.NodeRow{
		ID: "notes/foo-2", Type: "note", Path: "notes/foo-2.md", Title: "Foo2",
		PropertiesJSON: "{}", LastChecksum: "x",
	}); upsertErr != nil {
		test.Fatalf("upsert sibling: %v", upsertErr)
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      nodes,
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: queue,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	pane := report.SubUnitPane

	if pane == nil {
		test.Fatalf("SubUnitPane = nil, want populated")
	}

	if pane.Total != 5 {
		test.Errorf("Total = %d, want 5", pane.Total)
	}

	wantKinds := map[string]int{"section": 1, "paragraph": 2, "list-item": 1, "code-block": 1}

	for kind, want := range wantKinds {
		if got := pane.CountByKind[kind]; got != want {
			test.Errorf("CountByKind[%q] = %d, want %d", kind, got, want)
		}
	}

	if pane.HashCollisions != 1 {
		test.Errorf("HashCollisions = %d, want 1", pane.HashCollisions)
	}

	if pane.OrphanedSubUnits != 0 {
		test.Errorf("OrphanedSubUnits = %d, want 0", pane.OrphanedSubUnits)
	}

	if pane.EmbedQueueFiles != 1 {
		test.Errorf("EmbedQueueFiles = %d, want 1", pane.EmbedQueueFiles)
	}

	if pane.EmbedQueueSubUnits != 1 {
		test.Errorf("EmbedQueueSubUnits = %d, want 1", pane.EmbedQueueSubUnits)
	}

	if pane.OversizeEmbedPayloads != 1 {
		test.Errorf("OversizeEmbedPayloads = %d, want 1", pane.OversizeEmbedPayloads)
	}
}

// TestRun_SubUnitsDisabledDirtyEmitsWarning confirms doctor surfaces
// the IssueSubUnitsDisabledDirty issue when the manifest opts out but
// sub-unit rows remain in the index from a prior run with sub-units
// enabled.
func TestRun_SubUnitsDisabledDirtyEmitsWarning(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodes := index.NewNodeRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{
		ID: "notes/n", Type: "note", Path: "notes/n.md",
		PropertiesJSON: "{}", LastChecksum: "x",
	}); upsertErr != nil {
		test.Fatalf("upsert parent: %v", upsertErr)
	}

	parentID := sql.NullString{String: "notes/n", Valid: true}

	for idx := 0; idx < 2; idx++ {
		row := index.NodeRow{
			ID:             "notes/n#stale" + string(rune('a'+idx)),
			Type:           "paragraph",
			Path:           "notes/n.md",
			PropertiesJSON: "{}",
			LastChecksum:   "x",
			ParentID:       parentID,
			Ordinal:        sql.NullInt64{Int64: int64(idx), Valid: true},
			EmbedPayload:   sql.NullString{String: "stale", Valid: true},
		}

		if upsertErr := nodes.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert stale row: %v", upsertErr)
		}
	}

	loaded := loadSubUnitsDisabledManifest(test)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      nodes,
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Manifest:   loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.SubUnitPane == nil {
		test.Fatalf("SubUnitPane = nil, want populated for dirty state")
	}

	if report.SubUnitPane.Total != 2 {
		test.Errorf("SubUnitPane.Total = %d, want 2", report.SubUnitPane.Total)
	}

	sawDirty := false

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueSubUnitsDisabledDirty {
			sawDirty = true

			if !strings.Contains(issue.Message, "2") {
				test.Errorf("dirty issue message = %q, want it to mention the count", issue.Message)
			}
		}
	}

	if !sawDirty {
		test.Errorf("expected IssueSubUnitsDisabledDirty, got %+v", report.Issues)
	}
}
