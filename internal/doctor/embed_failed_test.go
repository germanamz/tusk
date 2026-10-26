package doctor_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// TestRun_SurfacesEmbedFailed pins #845's doctor side: a parked embed row
// (one that used up its attempts and stopped retrying) is a warning naming
// the node, the attempt count, the last error and what re-arms it. It is
// counted apart from the pending depth, and the same node is neither an
// embed-retry nor an embed-no-chunks finding.
func TestRun_SurfacesEmbedFailed(test *testing.T) {
	store, closer := newTempIndex(test)
	defer closer()

	nodes := index.NewNodeRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "notes/big", Type: "note", Title: "Big", Path: "notes/big.md", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	refusal := `ollama: HTTP 400: {"error":"the input length exceeds the context length"}`

	if _, execErr := store.DB().Exec(
		`INSERT INTO embed_queue (node_id, enqueued_at, attempts, last_error, kind, parked_at) VALUES (?, 1, 3, ?, 'embed', 2)`,
		"notes/big", refusal,
	); execErr != nil {
		test.Fatalf("seed parked row: %v", execErr)
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      nodes,
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Embeddings: index.NewEmbeddingRepo(store),
		Manifest:   &manifest.Manifest{Embeddings: manifest.EmbeddingsSection{Provider: "ollama"}},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedQueueDepth != 0 || report.EmbedFailed != 1 {
		test.Errorf("depth = %d, failed = %d; want 0 pending and 1 failed", report.EmbedQueueDepth, report.EmbedFailed)
	}

	var failed []doctor.Issue

	for _, issue := range report.Issues {
		switch issue.Kind {
		case doctor.IssueEmbedFailed:
			failed = append(failed, issue)
		case doctor.IssueEmbedRetry, doctor.IssueEmbedNoChunks:
			test.Errorf("parked node also reported as %s: %+v", issue.Kind, issue)
		}
	}

	if len(failed) != 1 {
		test.Fatalf("embed-failed issues = %+v, want one", failed)
	}

	issue := failed[0]

	if issue.NodeID != "notes/big" || issue.Severity != doctor.SeverityWarning {
		test.Errorf("issue = %+v, want a warning on notes/big", issue)
	}

	for _, want := range []string{"failed 3 times", "stopped retrying", refusal, "tusk reindex --force", "[embeddings]"} {
		if !strings.Contains(issue.Message, want) {
			test.Errorf("message %q lacks %q", issue.Message, want)
		}
	}
}

// TestRun_EmbedWaitingWithoutEmbeddings: with no [embeddings] section the
// queued rows wait for one, and the report says so.
func TestRun_EmbedWaitingWithoutEmbeddings(test *testing.T) {
	store, closer := newTempIndex(test)
	defer closer()

	queue := index.NewEmbedQueueRepo(store)

	if enqErr := queue.Enqueue("notes/a"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	for _, testCase := range []struct {
		name     string
		manifest *manifest.Manifest
		want     bool
	}{
		{name: "no embeddings section", manifest: &manifest.Manifest{}, want: true},
		{name: "embeddings configured", manifest: &manifest.Manifest{Embeddings: manifest.EmbeddingsSection{Provider: "ollama"}}, want: false},
	} {
		report, runErr := doctor.Run(doctor.Config{
			Nodes:      index.NewNodeRepo(store),
			EmbedQueue: queue,
			Manifest:   testCase.manifest,
		})

		if runErr != nil {
			test.Fatalf("%s: Run: %v", testCase.name, runErr)
		}

		if report.EmbedWaiting != testCase.want {
			test.Errorf("%s: EmbedWaiting = %v, want %v", testCase.name, report.EmbedWaiting, testCase.want)
		}
	}
}
