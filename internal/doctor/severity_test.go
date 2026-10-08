package doctor_test

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// TestSeverityFor_CoversEveryDeclaredKind parses the package source for every
// Issue* string constant and asserts the severity table classifies it, so a new
// kind cannot ship without a severity.
func TestSeverityFor_CoversEveryDeclaredKind(test *testing.T) {
	files, globErr := filepath.Glob("*.go")

	if globErr != nil {
		test.Fatalf("glob: %v", globErr)
	}

	fileSet := token.NewFileSet()
	kinds := map[string]string{}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		parsed, parseErr := parser.ParseFile(fileSet, file, nil, 0)

		if parseErr != nil {
			test.Fatalf("parse %s: %v", file, parseErr)
		}

		for _, decl := range parsed.Decls {
			general, ok := decl.(*ast.GenDecl)

			if !ok || general.Tok != token.CONST {
				continue
			}

			for _, spec := range general.Specs {
				valueSpec := spec.(*ast.ValueSpec)

				for position, name := range valueSpec.Names {
					if !strings.HasPrefix(name.Name, "Issue") || position >= len(valueSpec.Values) {
						continue
					}

					literal, isLiteral := valueSpec.Values[position].(*ast.BasicLit)

					if !isLiteral || literal.Kind != token.STRING {
						continue
					}

					value, unquoteErr := strconv.Unquote(literal.Value)

					if unquoteErr != nil {
						test.Fatalf("unquote %s: %v", name.Name, unquoteErr)
					}

					kinds[name.Name] = value
				}
			}
		}
	}

	if len(kinds) < 20 {
		test.Fatalf("found only %d Issue* constants; the source scan is broken", len(kinds))
	}

	for name, kind := range kinds {
		if _, ok := doctor.SeverityFor(kind); !ok {
			test.Errorf("%s (%q) has no severity in the table", name, kind)
		}
	}
}

func TestSeverityFor_Placements(test *testing.T) {
	for kind, want := range map[string]string{
		doctor.IssueDanglingEdge:              doctor.SeverityError,
		doctor.IssueSkippedFile:               doctor.SeverityError,
		doctor.IssueGraphExpansionInvalidEdge: doctor.SeverityError,
		doctor.IssueAliasInvalid:              doctor.SeverityError,
		doctor.IssueContextPinnedMissing:      doctor.SeverityError,
		doctor.IssueUndeclaredType:            doctor.SeverityWarning,
		doctor.IssueEmbedNoChunks:             doctor.SeverityWarning,
		doctor.IssueLegacyCLIEdge:             doctor.SeverityWarning,
		doctor.IssueEmbedLargeChunk:           doctor.SeverityAdvice,
		doctor.IssueEmbeddingPrefixHint:       doctor.SeverityAdvice,
	} {
		if got, _ := doctor.SeverityFor(kind); got != want {
			test.Errorf("SeverityFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

// doctorFixture is an index plus the repos doctor reads.
type doctorFixture struct {
	store      *index.Index
	nodes      *index.NodeRepo
	edges      *index.EdgeRepo
	fileStates *index.FileStateRepo
}

func newDoctorFixture(test *testing.T) *doctorFixture {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return &doctorFixture{
		store:      store,
		nodes:      index.NewNodeRepo(store),
		edges:      index.NewEdgeRepo(store),
		fileStates: index.NewFileStateRepo(store),
	}
}

func (fixture *doctorFixture) config(loaded *manifest.Manifest) doctor.Config {
	return doctor.Config{
		Nodes:      fixture.nodes,
		Edges:      fixture.edges,
		EmbedQueue: index.NewEmbedQueueRepo(fixture.store),
		FileStates: fixture.fileStates,
		Manifest:   loaded,
	}
}

func (fixture *doctorFixture) file(test *testing.T, id, nodeType string) {
	test.Helper()

	if upsertErr := fixture.nodes.Upsert(index.NodeRow{
		ID: id, Type: nodeType, Path: id + ".md", Title: id, PropertiesJSON: "{}", LastChecksum: "x",
	}); upsertErr != nil {
		test.Fatalf("upsert %s: %v", id, upsertErr)
	}
}

func (fixture *doctorFixture) subUnits(test *testing.T, parent string, ids ...string) {
	test.Helper()

	rows := make([]index.NodeRow, 0, len(ids))

	for position, id := range ids {
		rows = append(rows, index.NodeRow{
			ID: id, Type: "paragraph", Path: parent + ".md", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: sql.NullString{String: parent, Valid: true},
			Ordinal:  sql.NullInt64{Int64: int64(position), Valid: true},
		})
	}

	if bulkErr := fixture.nodes.BulkUpsert(rows, "markdown"); bulkErr != nil {
		test.Fatalf("bulk upsert sub-units: %v", bulkErr)
	}
}

func run(test *testing.T, config doctor.Config) *doctor.Report {
	test.Helper()

	report, runErr := doctor.Run(config)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return report
}

func TestRun_StampsSeverityOrdersErrorsFirstAndCounts(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "docs/a", "note")
	fixture.file(test, "docs/b", "notee")
	fixture.edges.UpsertAll("docs/a", "docs/a.md", []index.EdgeRow{
		{Type: "references", SourceID: "docs/a", TargetID: "docs/nowhere", SourcePath: "docs/a.md", Kind: "direct"},
	})

	// The prefix hint (advice) is computed before the dangling check (error)
	// and the undeclared-type check (warning); the report must still lead
	// with the error.
	report := run(test, fixture.config(&manifest.Manifest{
		NodeTypes:  map[string]manifest.NodeType{"note": {}},
		Embeddings: manifest.EmbeddingsSection{Provider: "ollama", Model: "nomic-embed-text"},
	}))

	var got []string

	for _, issue := range report.Issues {
		got = append(got, issue.Severity+":"+issue.Kind)
	}

	want := []string{
		"error:" + doctor.IssueDanglingEdge,
		"warning:" + doctor.IssueUndeclaredType,
		"advice:" + doctor.IssueEmbeddingPrefixHint,
	}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("issues = %v, want %v", got, want)
	}

	if counts := report.Counts(); counts != (doctor.Counts{Errors: 1, Warnings: 1, Advice: 1}) {
		test.Errorf("Counts = %+v, want 1/1/1", counts)
	}
}

// TestRun_DanglingEdgeReportedOncePerFile pins #759 item 3: a dangling link in
// a paragraph is carried by the file row and by every sub-unit above it, but
// it is one finding, located at the file with the sub-units as locations.
func TestRun_DanglingEdgeReportedOncePerFile(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "docs/dangling", "note")
	fixture.subUnits(test, "docs/dangling", "docs/dangling#S1", "docs/dangling#S1P1")

	for _, source := range []string{"docs/dangling", "docs/dangling#S1", "docs/dangling#S1P1"} {
		fixture.edges.UpsertAll(source, "docs/dangling.md", []index.EdgeRow{
			{Type: "references", SourceID: source, TargetID: "docs/nowhere", SourcePath: "docs/dangling.md", Kind: "direct"},
		})
	}

	// A different target from the same file is a separate finding.
	fixture.edges.UpsertAll("docs/dangling#S1P1", "docs/dangling.md", []index.EdgeRow{
		{Type: "references", SourceID: "docs/dangling#S1P1", TargetID: "docs/nowhere", SourcePath: "docs/dangling.md", Kind: "direct"},
		{Type: "references", SourceID: "docs/dangling#S1P1", TargetID: "docs/elsewhere", SourcePath: "docs/dangling.md", Kind: "direct"},
	})

	dangling := issuesOfKind(run(test, fixture.config(nil)), doctor.IssueDanglingEdge)

	if len(dangling) != 2 {
		test.Fatalf("dangling issues = %+v, want 2 (one per target)", dangling)
	}

	byTarget := map[string]doctor.Issue{}

	for _, issue := range dangling {
		if issue.NodeID != "docs/dangling" {
			test.Errorf("NodeID = %q, want the file id docs/dangling", issue.NodeID)
		}

		byTarget[issue.Message] = issue
	}

	nowhere := byTarget[`edge "references" -> "docs/nowhere" (target missing)`]

	if want := []string{"docs/dangling#S1", "docs/dangling#S1P1"}; !reflect.DeepEqual(nowhere.Locations, want) {
		test.Errorf("nowhere Locations = %v, want %v", nowhere.Locations, want)
	}

	elsewhere := byTarget[`edge "references" -> "docs/elsewhere" (target missing)`]

	if want := []string{"docs/dangling#S1P1"}; !reflect.DeepEqual(elsewhere.Locations, want) {
		test.Errorf("elsewhere Locations = %v, want %v", elsewhere.Locations, want)
	}
}

func TestRun_UndeclaredTypeGroupedPerType(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "docs/a", "note")
	fixture.file(test, "docs/c", "notee")
	fixture.file(test, "docs/b", "notee")
	fixture.file(test, "docs/d", "widget")
	// Sub-unit types are structural, never declared by the user.
	fixture.subUnits(test, "docs/a", "docs/a#S1")

	undeclared := issuesOfKind(run(test, fixture.config(&manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"note": {}},
	})), doctor.IssueUndeclaredType)

	if len(undeclared) != 2 {
		test.Fatalf("undeclared-type issues = %+v, want 2 (notee, widget)", undeclared)
	}

	if !strings.Contains(undeclared[0].Message, `"notee"`) ||
		!reflect.DeepEqual(undeclared[0].Locations, []string{"docs/b", "docs/c"}) {
		test.Errorf("first = %+v, want notee at docs/b, docs/c", undeclared[0])
	}

	if !strings.Contains(undeclared[1].Message, `"widget"`) ||
		!reflect.DeepEqual(undeclared[1].Locations, []string{"docs/d"}) {
		test.Errorf("second = %+v, want widget at docs/d", undeclared[1])
	}
}

// TestRun_NoUndeclaredTypeForSchemalessManifest pins that a vault declaring no
// node types at all (what `tusk init` writes) is schemaless by choice, not a
// vault full of typos.
func TestRun_NoUndeclaredTypeForSchemalessManifest(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "docs/a", "note")

	// The loader merges the built-in sub-document types into NodeTypes when
	// sub-units are on; they do not make the vault schema'd.
	for name, loaded := range map[string]*manifest.Manifest{
		"empty":                  {},
		"only built-in subtypes": {NodeTypes: manifest.SubdocumentNodeTypes()},
	} {
		report := run(test, fixture.config(loaded))

		if undeclared := issuesOfKind(report, doctor.IssueUndeclaredType); len(undeclared) != 0 {
			test.Errorf("%s: undeclared-type issues = %+v, want none for a schemaless manifest", name, undeclared)
		}
	}
}

func TestRun_ReportsSkippedFiles(test *testing.T) {
	fixture := newDoctorFixture(test)

	// docs/doc indexed once, then broke: its stale row is still served.
	fixture.file(test, "docs/doc", "note")
	// notes/y has a section sub-unit whose id the reserved file aliases.
	fixture.file(test, "notes/y", "note")
	fixture.subUnits(test, "notes/y", "notes/y#S1")

	for path, reason := range map[string]string{
		"docs/broken.md": "node: decode frontmatter docs/broken.md: yaml: line 1: did not find expected node content",
		"docs/doc.md":    "node: decode frontmatter docs/doc.md: yaml: bad",
		"notes/y#S1.md":  `reserved id: id "notes/y#S1" contains "#", the reserved sub-unit separator`,
	} {
		if recordErr := fixture.fileStates.RecordSkip(path, reason); recordErr != nil {
			test.Fatalf("RecordSkip: %v", recordErr)
		}
	}

	skipped := issuesOfKind(run(test, fixture.config(nil)), doctor.IssueSkippedFile)

	if len(skipped) != 3 {
		test.Fatalf("skipped-file issues = %+v, want 3", skipped)
	}

	byPath := map[string]doctor.Issue{}

	for _, issue := range skipped {
		if issue.Severity != doctor.SeverityError {
			test.Errorf("%s severity = %q, want error", issue.NodeID, issue.Severity)
		}

		byPath[issue.NodeID] = issue
	}

	const stale = "the index still serves the last version that parsed"

	if broken := byPath["docs/broken.md"]; !strings.HasPrefix(broken.Message, "node: decode frontmatter") ||
		strings.Contains(broken.Message, stale) {
		test.Errorf("broken = %+v, want the decode error with no stale note", broken)
	}

	if doc := byPath["docs/doc.md"]; !strings.Contains(doc.Message, stale) {
		test.Errorf("doc = %+v, want the stale-row note", doc)
	}

	// notes/y#S1 is a live sub-unit id, but not this file's row.
	if reserved := byPath["notes/y#S1.md"]; strings.Contains(reserved.Message, stale) {
		test.Errorf("reserved = %+v, must not claim a stale row through a sibling's sub-unit id", reserved)
	}
}

func TestRun_FoldsManifestAndPinnedErrorsIntoIssues(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "docs/kept", "note")

	report := run(test, fixture.config(&manifest.Manifest{
		AliasErrors:   []manifest.AliasError{{Name: "bad", Message: "unknown verb \"nope\""}},
		ContextErrors: []manifest.ContextError{{Message: "context: recent: unknown alias \"gone\""}},
		Context:       &manifest.Context{Pinned: []string{"docs/kept", "docs/renamed-away"}},
	}))

	alias := issuesOfKind(report, doctor.IssueAliasInvalid)

	if len(alias) != 1 || alias[0].NodeID != "bad" || !strings.Contains(alias[0].Message, "nope") {
		test.Errorf("alias-invalid = %+v, want one for alias bad", alias)
	}

	context := issuesOfKind(report, doctor.IssueContextInvalid)

	if len(context) != 1 || !strings.Contains(context[0].Message, "gone") {
		test.Errorf("context-invalid = %+v, want one", context)
	}

	pinned := issuesOfKind(report, doctor.IssueContextPinnedMissing)

	if len(pinned) != 1 || pinned[0].NodeID != "docs/renamed-away" {
		test.Errorf("context-pinned-missing = %+v, want one for docs/renamed-away", pinned)
	}

	if counts := report.Counts(); counts.Errors != 3 {
		test.Errorf("Counts.Errors = %d, want 3", counts.Errors)
	}
}

// TestRunWithMigration_ReportsUnmigratedLegacyRowsAsIssues pins that a legacy
// row the migration pass leaves in place surfaces as a legacy-*-edge warning
// naming the specific reason, not as a side-channel string.
func TestRunWithMigration_ReportsUnmigratedLegacyRowsAsIssues(test *testing.T) {
	fixture := newDoctorFixture(test)
	root := test.TempDir()

	fixture.file(test, "docs/gone", "note")
	fixture.file(test, "docs/target", "note")

	fixture.edges.UpsertAll("docs/gone", index.CLISourcePath, []index.EdgeRow{
		{Type: "undeclared", SourceID: "docs/gone", TargetID: "docs/target", SourcePath: index.CLISourcePath, Kind: "direct"},
	})
	fixture.edges.UpsertAll("docs/gone", index.MCPSourcePath, []index.EdgeRow{
		{Type: "references", SourceID: "docs/gone", TargetID: "docs/target", SourcePath: index.MCPSourcePath, Kind: "direct"},
	})

	config := fixture.config(&manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"note": {}},
		EdgeTypes: manifest.EdgeTypes{"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany,
		}},
	})
	config.Root = root

	result, runErr := doctor.RunWithMigration(doctor.Request{Cfg: config})

	if runErr != nil {
		test.Fatalf("RunWithMigration: %v", runErr)
	}

	cli := issuesOfKind(result.Report, doctor.IssueLegacyCLIEdge)

	if len(cli) != 1 || cli[0].NodeID != "docs/gone" || !strings.Contains(cli[0].Message, `edge type "undeclared" not declared`) {
		test.Errorf("legacy-cli-edge = %+v, want the undeclared-type row", cli)
	}

	mcp := issuesOfKind(result.Report, doctor.IssueLegacyMCPEdge)

	if len(mcp) != 1 || !strings.Contains(mcp[0].Message, "source file docs/gone.md not found") {
		test.Errorf("legacy-mcp-edge = %+v, want the missing-source row", mcp)
	}

	for _, issue := range append(cli, mcp...) {
		if issue.Severity != doctor.SeverityWarning {
			test.Errorf("%s severity = %q, want warning", issue.Kind, issue.Severity)
		}
	}
}
