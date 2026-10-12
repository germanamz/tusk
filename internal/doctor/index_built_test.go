package doctor_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
)

func indexNotBuiltIssues(report *doctor.Report) []doctor.Issue {
	var issues []doctor.Issue

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueIndexNotBuilt {
			issues = append(issues, issue)
		}
	}

	return issues
}

// An index no reindex pass has written may be missing every file on disk, so
// a clean report from it proves nothing (#787): doctor says so as a warning.
func TestRun_WarnsWhenIndexNeverBuilt(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	meta := index.NewMetaRepo(store)
	config := doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Meta:       meta,
	}

	report, runErr := doctor.Run(config)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	issues := indexNotBuiltIssues(report)

	if len(issues) != 1 {
		test.Fatalf("index-not-built issues = %+v, want exactly one", issues)
	}

	if issues[0].Severity != doctor.SeverityWarning {
		test.Errorf("severity = %q, want %q", issues[0].Severity, doctor.SeverityWarning)
	}

	if setErr := meta.Set(index.LastReindexAtKey, "1"); setErr != nil {
		test.Fatalf("meta Set: %v", setErr)
	}

	report, runErr = doctor.Run(config)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if issues := indexNotBuiltIssues(report); len(issues) != 0 {
		test.Errorf("index-not-built issues after a reindex = %+v, want none", issues)
	}
}
