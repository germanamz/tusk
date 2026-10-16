package doctor_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

func indexSchemaStaleIssues(test *testing.T, config doctor.Config) []doctor.Issue {
	test.Helper()

	report, runErr := doctor.Run(config)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	var issues []doctor.Issue

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueIndexSchemaStale {
			issues = append(issues, issue)
		}
	}

	return issues
}

// After an edit to tusk.toml's types and before the next reindex, the index's
// edges and drift still follow the old declarations, and every drift check
// reports against them (#831): doctor says so as a warning.
func TestRun_WarnsWhenIndexSchemaStale(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	built := &manifest.Manifest{NodeTypes: map[string]manifest.NodeType{"note": {}}}
	edited := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"note": {
			Properties: []manifest.PropertyDecl{{Name: "owner", Type: "ref", To: "note"}},
		}},
	}

	meta := index.NewMetaRepo(store)
	config := doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Meta:       meta,
		Manifest:   edited,
	}

	if issues := indexSchemaStaleIssues(test, config); len(issues) != 0 {
		test.Errorf("index-schema-stale issues on an unbuilt index = %+v, want none (index-not-built covers it)", issues)
	}

	for key, value := range map[string]string{
		index.LastReindexAtKey:  "1",
		index.ManifestSchemaKey: built.SchemaFingerprint(),
	} {
		if setErr := meta.Set(key, value); setErr != nil {
			test.Fatalf("meta Set %s: %v", key, setErr)
		}
	}

	issues := indexSchemaStaleIssues(test, config)

	if len(issues) != 1 {
		test.Fatalf("index-schema-stale issues = %+v, want exactly one", issues)
	}

	if issues[0].Severity != doctor.SeverityWarning {
		test.Errorf("severity = %q, want %q", issues[0].Severity, doctor.SeverityWarning)
	}

	config.Manifest = built

	if issues := indexSchemaStaleIssues(test, config); len(issues) != 0 {
		test.Errorf("index-schema-stale issues with the schema the index was built under = %+v, want none", issues)
	}
}
