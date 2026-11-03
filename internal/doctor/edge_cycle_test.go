package doctor_test

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// A hand-written cycle on an acyclic edge type passes reindex, so doctor
// reports it (#852): once per group of nodes that reach each other, at the
// edge type's level, with the shortest cycle through the group's first node.
func TestRun_ReportsCyclesOnAcyclicEdgeTypes(test *testing.T) {
	ids := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "m", "n", "p", "q", "x"}
	nodes := make([]index.NodeRow, 0, len(ids))

	for _, id := range ids {
		nodes = append(nodes, index.NodeRow{ID: id, Type: "ticket", Path: id + ".md"})
	}

	for _, fileID := range []string{"h", "k"} {
		nodes = append(nodes, index.NodeRow{
			ID: fileID + "#S1", Type: "section", Path: fileID + ".md",
			ParentID: sql.NullString{String: fileID, Valid: true},
		})
	}

	edge := func(edgeType, source, target, kind string) index.EdgeRow {
		sourcePath := source + ".md"

		if len(source) > 1 {
			sourcePath = source[:1] + ".md"
		}

		return index.EdgeRow{Type: edgeType, SourceID: source, TargetID: target, SourcePath: sourcePath, Kind: kind}
	}

	config := seedEdgeConstraintIndex(test, nodes,
		[]index.EdgeRow{
			edge("depends-on", "a", "b", "direct"),
			edge("depends-on", "b", "a", "direct"),
			edge("depends-on", "c", "c", "direct"),
			// d, e and f reach each other; the shortest cycle from d skips f.
			edge("depends-on", "d", "e", "direct"),
			edge("depends-on", "e", "f", "direct"),
			edge("depends-on", "e", "d", "direct"),
			edge("depends-on", "f", "d", "direct"),
			// g reaches the a/b cycle but is not on it.
			edge("depends-on", "g", "a", "direct"),
			// A sub-unit's copy of a body link is the file row's to report;
			// with no file row there is no cycle.
			edge("depends-on", "h#S1", "i", "direct"),
			edge("depends-on", "i", "h", "direct"),
			// A link to a heading points into the file that holds it.
			edge("depends-on", "j", "k#S1", "direct"),
			edge("depends-on", "k", "j", "direct"),
			// A ref property's edges are derived rows.
			edge("replaces", "m", "n", "derived"),
			edge("replaces", "n", "m", "derived"),
			// Cycles are allowed here.
			edge("blocks", "p", "q", "direct"),
			edge("blocks", "q", "p", "direct"),
			// Structural rows can't loop; the built-in contains is skipped.
			{Type: "contains", SourceID: "x", TargetID: "x", SourcePath: "x.md", Kind: "structural", Source: sql.NullString{String: "markdown", Valid: true}},
		},
		manifest.EdgeTypes{
			"depends-on": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany, Acyclic: manifest.AcyclicError},
			"replaces":   {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany, Acyclic: manifest.AcyclicWarning},
			"blocks":     {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany},
			"contains":   {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityOneToMany, Acyclic: manifest.AcyclicError},
		},
	)

	report, runErr := doctor.Run(config)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	want := []doctor.Issue{
		{Kind: doctor.IssueEdgeCycle, Severity: doctor.SeverityError, NodeID: "a", Message: `edge "depends-on" is acyclic, but these nodes form a cycle: a → b → a`},
		{Kind: doctor.IssueEdgeCycle, Severity: doctor.SeverityError, NodeID: "c", Message: `edge "depends-on" is acyclic, but these nodes form a cycle: c → c`},
		{Kind: doctor.IssueEdgeCycle, Severity: doctor.SeverityError, NodeID: "d", Message: `edge "depends-on" is acyclic, but these nodes form a cycle: d → e → d`, Locations: []string{"f"}},
		{Kind: doctor.IssueEdgeCycle, Severity: doctor.SeverityError, NodeID: "j", Message: `edge "depends-on" is acyclic, but these nodes form a cycle: j → k → j`},
		{Kind: doctor.IssueEdgeCycle, Severity: doctor.SeverityWarning, NodeID: "m", Message: `edge "replaces" is acyclic, but these nodes form a cycle: m → n → m`},
	}

	if got := issuesOfKind(report, doctor.IssueEdgeCycle); !reflect.DeepEqual(got, want) {
		test.Fatalf("edge-cycle issues =\n%+v\nwant\n%+v", got, want)
	}
}
