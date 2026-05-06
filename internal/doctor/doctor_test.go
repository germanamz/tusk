package doctor_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
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
