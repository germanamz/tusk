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
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/missing", SourcePath: "tickets/a.md", Kind: "direct"},
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

func TestRun_WorkflowViolationUsesPersistedDetail(test *testing.T) {
	store, closer := newTempIndex(test)
	defer closer()

	driftRepo := index.NewWorkflowDriftRepo(store)

	const detail = "workflow \"tickets\": cannot transition status \"active\" → \"completed\"\n  valid targets from \"active\": done"

	if appendErr := driftRepo.Append(index.WorkflowDriftRow{
		NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "completed", Property: "status",
		ErrorCode: "illegal-transition", Detail: detail, ObservedAt: 100,
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

	var msg string

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueWorkflowViolation && issue.NodeID == "tickets/foo" {
			msg = issue.Message
		}
	}

	if !strings.Contains(msg, "cannot transition") {
		test.Errorf("Issue.Message = %q, want the persisted detail (mentions 'cannot transition')", msg)
	}

	if strings.Contains(msg, "is not a declared state") {
		test.Errorf("Issue.Message = %q, want persisted detail, not the hardcoded fallback", msg)
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

func TestRun_EmbedNoChunks_SkipsSectionsFlagsMissingLeaf(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	embedQueue := index.NewEmbedQueueRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	// Parent file (embedded so its own row isn't flagged).
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "notes/f", Type: "note", Title: "F", Path: "notes/f.md", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert file: %v", upsertErr)
	}

	if upsertErr := embeddings.Upsert(index.EmbeddingRow{NodeID: "notes/f", ChunkIdx: 0, Model: "m", ContentHash: "hf", Vector: []float32{0.1}, Dim: 1, Body: "x"}); upsertErr != nil {
		test.Fatalf("embed file: %v", upsertErr)
	}

	sub := func(id, typ, hash string) index.NodeRow {
		return index.NodeRow{
			ID: id, Type: typ, Title: id, Path: "notes/f.md", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID:    sql.NullString{String: "notes/f", Valid: true},
			Ordinal:     sql.NullInt64{Int64: 0, Valid: true},
			ContentHash: sql.NullString{String: hash, Valid: hash != ""},
		}
	}

	if upsertErr := nodes.BulkUpsert([]index.NodeRow{
		sub("notes/f#S1", "section", ""),          // never embedded → must NOT flag
		sub("notes/f#S1P1", "paragraph", "hp"),    // embedded below → must NOT flag
		sub("notes/f#S1P2", "paragraph", "hmiss"), // no embedding, not pending → must flag
	}); upsertErr != nil {
		test.Fatalf("bulk upsert subs: %v", upsertErr)
	}

	if upsertErr := embeddings.Upsert(index.EmbeddingRow{NodeID: "notes/f#S1P1", ChunkIdx: 0, Model: "m", ContentHash: "hp", Vector: []float32{0.2}, Dim: 1, Body: "p"}); upsertErr != nil {
		test.Fatalf("embed leaf: %v", upsertErr)
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

	flagged := map[string]bool{}

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueEmbedNoChunks {
			flagged[issue.NodeID] = true
		}
	}

	if flagged["notes/f#S1"] {
		test.Error("section notes/f#S1 must not be flagged embed-no-chunks (#513)")
	}

	if flagged["notes/f#S1P1"] {
		test.Error("embedded leaf notes/f#S1P1 must not be flagged")
	}

	if !flagged["notes/f#S1P2"] {
		test.Error("genuinely missing leaf notes/f#S1P2 must still be flagged")
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

// TestRun_GraphExpansionPaneAbsentWithoutManifest confirms the typed
// pane stays nil when no manifest is supplied (e.g., the fresh-index
// fixtures used elsewhere in this file).
func TestRun_GraphExpansionPaneAbsentWithoutManifest(test *testing.T) {
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

	if report.GraphExpansion != nil {
		test.Errorf("GraphExpansion = %+v, want nil for nil manifest", report.GraphExpansion)
	}
}

// loadGraphExpansionManifest writes a tusk.toml with the given body and
// returns the loaded *Manifest after MergeBuiltinPacks has run, mirroring
// how cmd_doctor.go prepares the manifest before calling doctor.Run.
func loadGraphExpansionManifest(test *testing.T, body string) *manifest.Manifest {
	test.Helper()

	path := filepath.Join(test.TempDir(), "tusk.toml")

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	manifest.MergeBuiltinPacks(loaded)

	return loaded
}

// TestRun_GraphExpansionPaneDefaults_WithBuiltinContains confirms a
// manifest with no [query.graph-expansion] block populates the pane with
// DefaultGraphExpansion values. With sub-units on (default),
// MergeBuiltinPacks injects `contains` so it is NOT flagged as unknown;
// `references` and `parent` are not declared in the bare manifest and
// are flagged as unknown. No Issues should be emitted because Enabled
// defaults to false.
func TestRun_GraphExpansionPaneDefaults_WithBuiltinContains(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	loaded := loadGraphExpansionManifest(test, `
[workspace]
name = "x"
`)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Manifest:   loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	pane := report.GraphExpansion

	if pane == nil {
		test.Fatalf("GraphExpansion = nil, want populated for manifest with no block")
	}

	if pane.Enabled {
		test.Errorf("Enabled = true, want false (default)")
	}

	if pane.Hops != 1 {
		test.Errorf("Hops = %d, want 1 (default)", pane.Hops)
	}

	if pane.Weight != 0.2 {
		test.Errorf("Weight = %v, want 0.2 (default)", pane.Weight)
	}

	if pane.CandidateMultiplier != 5 {
		test.Errorf("CandidateMultiplier = %d, want 5 (default)", pane.CandidateMultiplier)
	}

	containsUnknown := false

	for _, name := range pane.UnknownEdgeTypes {
		if name == "contains" {
			containsUnknown = true
		}
	}

	if containsUnknown {
		test.Errorf("UnknownEdgeTypes contains \"contains\" but sub-units pack should have injected it: %v", pane.UnknownEdgeTypes)
	}

	// references, parent, tagged are not declared in the bare manifest;
	// they should appear as unknown.
	for _, want := range []string{"references", "parent", "tagged"} {
		found := false

		for _, name := range pane.UnknownEdgeTypes {
			if name == want {
				found = true
			}
		}

		if !found {
			test.Errorf("UnknownEdgeTypes missing %q; got %v", want, pane.UnknownEdgeTypes)
		}
	}

	// No Issues should be emitted: feature is disabled by default.
	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueGraphExpansionUnknownEdge || issue.Kind == doctor.IssueGraphExpansionWeightZero {
			test.Errorf("expected no graph-expansion Issues when Enabled=false; got %+v", issue)
		}
	}
}

// TestRun_GraphExpansionPaneEnabledSurfacesUnknownEdge confirms that
// when Enabled=true, unknown edge types both populate
// pane.UnknownEdgeTypes and emit one IssueGraphExpansionUnknownEdge per
// entry.
func TestRun_GraphExpansionPaneEnabledSurfacesUnknownEdge(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	loaded := loadGraphExpansionManifest(test, `
[workspace]
name = "x"

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"

[query.graph-expansion]
enabled = true
edge-types = ["references", "contains", "made-up-edge"]
`)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Manifest:   loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	pane := report.GraphExpansion

	if pane == nil {
		test.Fatalf("GraphExpansion = nil, want populated")
	}

	if !pane.Enabled {
		test.Errorf("Enabled = false, want true")
	}

	if len(pane.UnknownEdgeTypes) != 1 || pane.UnknownEdgeTypes[0] != "made-up-edge" {
		test.Errorf("UnknownEdgeTypes = %v, want [made-up-edge]", pane.UnknownEdgeTypes)
	}

	unknownIssues := 0

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueGraphExpansionUnknownEdge {
			unknownIssues++

			if issue.NodeID != "made-up-edge" {
				test.Errorf("Issue.NodeID = %q, want \"made-up-edge\"", issue.NodeID)
			}

			if !strings.Contains(issue.Message, "made-up-edge") {
				test.Errorf("Issue.Message = %q, want mention of made-up-edge", issue.Message)
			}
		}
	}

	if unknownIssues != 1 {
		test.Errorf("IssueGraphExpansionUnknownEdge count = %d, want 1", unknownIssues)
	}
}

// TestRun_GraphExpansionPaneSortsUnknownEdgeTypes confirms that
// UnknownEdgeTypes is emitted in lexicographic order regardless of the
// on-disk TOML iteration order, and that the matching
// IssueGraphExpansionUnknownEdge entries follow the same sorted order.
// This guards against non-deterministic CLI output and Issue ordering
// when two or more unknown edge types are configured.
func TestRun_GraphExpansionPaneSortsUnknownEdgeTypes(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	loaded := loadGraphExpansionManifest(test, `
[workspace]
name = "x"

[query.graph-expansion]
enabled = true
edge-types = ["z-extra", "a-extra"]
`)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Manifest:   loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	pane := report.GraphExpansion

	if pane == nil {
		test.Fatalf("GraphExpansion = nil, want populated")
	}

	wantUnknown := []string{"a-extra", "z-extra"}

	if len(pane.UnknownEdgeTypes) != len(wantUnknown) {
		test.Fatalf("UnknownEdgeTypes = %v, want %v", pane.UnknownEdgeTypes, wantUnknown)
	}

	for index, got := range pane.UnknownEdgeTypes {
		if got != wantUnknown[index] {
			test.Errorf("UnknownEdgeTypes[%d] = %q, want %q (full slice: %v)", index, got, wantUnknown[index], pane.UnknownEdgeTypes)
		}
	}

	var unknownIDs []string

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueGraphExpansionUnknownEdge {
			unknownIDs = append(unknownIDs, issue.NodeID)
		}
	}

	if len(unknownIDs) != len(wantUnknown) {
		test.Fatalf("IssueGraphExpansionUnknownEdge NodeIDs = %v, want %v", unknownIDs, wantUnknown)
	}

	for index, got := range unknownIDs {
		if got != wantUnknown[index] {
			test.Errorf("IssueGraphExpansionUnknownEdge[%d].NodeID = %q, want %q (full slice: %v)", index, got, wantUnknown[index], unknownIDs)
		}
	}
}

// TestRun_GraphExpansionPaneFlagsWeightZeroNoOp confirms the WeightZeroNoOp
// flag and matching Issue light up when Enabled=true && Weight=0.
func TestRun_GraphExpansionPaneFlagsWeightZeroNoOp(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	loaded := loadGraphExpansionManifest(test, `
[workspace]
name = "x"

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"

[query.graph-expansion]
enabled = true
weight = 0.0
edge-types = ["references", "contains"]
`)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Manifest:   loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	pane := report.GraphExpansion

	if pane == nil {
		test.Fatalf("GraphExpansion = nil, want populated")
	}

	if !pane.WeightZeroNoOp {
		test.Errorf("WeightZeroNoOp = false, want true")
	}

	sawZero := false

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueGraphExpansionWeightZero {
			sawZero = true

			if !strings.Contains(issue.Message, "no-op") {
				test.Errorf("Issue.Message = %q, want mention of no-op", issue.Message)
			}
		}
	}

	if !sawZero {
		test.Errorf("expected IssueGraphExpansionWeightZero, got %+v", report.Issues)
	}
}

// TestRun_GraphExpansionPaneDisabledSuppressesIssues confirms that a
// configured-but-disabled block still populates UnknownEdgeTypes in the
// typed pane but emits no Issues — warnings for an off feature are noise.
func TestRun_GraphExpansionPaneDisabledSuppressesIssues(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	loaded := loadGraphExpansionManifest(test, `
[workspace]
name = "x"

[query.graph-expansion]
enabled = false
weight = 0.0
edge-types = ["made-up-edge"]
`)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Manifest:   loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	pane := report.GraphExpansion

	if pane == nil {
		test.Fatalf("GraphExpansion = nil, want populated even when disabled")
	}

	if len(pane.UnknownEdgeTypes) != 1 || pane.UnknownEdgeTypes[0] != "made-up-edge" {
		test.Errorf("UnknownEdgeTypes = %v, want [made-up-edge]", pane.UnknownEdgeTypes)
	}

	if pane.WeightZeroNoOp {
		test.Errorf("WeightZeroNoOp = true, want false when Enabled=false")
	}

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueGraphExpansionUnknownEdge || issue.Kind == doctor.IssueGraphExpansionWeightZero {
			test.Errorf("expected no graph-expansion Issues when Enabled=false; got %+v", issue)
		}
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

	if upsertErr := nodes.BulkUpsert(subRows); upsertErr != nil {
		test.Fatalf("bulk upsert sub-units: %v", upsertErr)
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

	staleRows := make([]index.NodeRow, 0, 2)

	for idx := range 2 {
		staleRows = append(staleRows, index.NodeRow{
			ID:             "notes/n#stale" + string(rune('a'+idx)),
			Type:           "paragraph",
			Path:           "notes/n.md",
			PropertiesJSON: "{}",
			LastChecksum:   "x",
			ParentID:       parentID,
			Ordinal:        sql.NullInt64{Int64: int64(idx), Valid: true},
			EmbedPayload:   sql.NullString{String: "stale", Valid: true},
		})
	}

	if upsertErr := nodes.BulkUpsert(staleRows); upsertErr != nil {
		test.Fatalf("bulk upsert stale rows: %v", upsertErr)
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
