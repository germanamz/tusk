package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
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
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/missing", Ordinal: 0, SourcePath: "tickets/a.md"},
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
