package doctor_test

import (
	"path/filepath"
	"strings"
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

func newTempIndex(test *testing.T) (*index.Index, func()) {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store, func() { store.Close() }
}
