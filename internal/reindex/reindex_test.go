package reindex_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

func TestRun_IndexesAllMarkdownNodes(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/auth.md", "type: note\ntitle: Auth\n", "Body.\n")
	writeNode(test, root, "tickets/fix.md", "type: ticket\ntitle: Fix\n", "Body.\n")
	writeNode(test, root, "ignored.txt", "", "not markdown")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	repo := index.NewNodeRepo(store)

	report, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	loaded, listErr := repo.List(index.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(loaded) != 2 {
		test.Errorf("len = %d, want 2", len(loaded))
	}
}

func TestRun_SkipsTuskInternalDir(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, ".tusk", "fake.md"), []byte("---\ntype: note\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	writeNode(test, root, "real.md", "type: note\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)

	_, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	loaded, _ := repo.List(index.ListFilter{})

	if len(loaded) != 1 || loaded[0].ID != "real" {
		test.Errorf("unexpected: %+v", loaded)
	}
}

func TestRun_RemovesStaleEntries(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "stale.md", "type: note\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)

	if _, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo}); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	if rmErr := os.Remove(filepath.Join(root, "stale.md")); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	report, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	if report.Removed != 1 {
		test.Errorf("Removed = %d, want 1", report.Removed)
	}

	if _, getErr := repo.Get("stale"); getErr != index.ErrNodeNotFound {
		test.Errorf("err = %v, want ErrNodeNotFound", getErr)
	}
}

func TestRun_PersistsFrontmatterEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "tickets/epic.md", "type: ticket\ntitle: Epic\n", "Body.\n")
	writeNode(test, root, "tickets/child.md", "type: ticket\ntitle: Child\nparent: tickets/epic\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket", "project"},
			Cardinality: manifest.CardinalityManyToOne,
		},
	}

	report, runErr := reindex.Run(reindex.Config{
		Root:      root,
		Repo:      repo,
		Edges:     edgeRepo,
		EdgeTypes: edgeTypes,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	listed, _ := edgeRepo.ListBySource("tickets/child")

	if len(listed) != 1 || listed[0].Type != "parent" || listed[0].TargetID != "tickets/epic" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestRun_PersistsWikilinksAsReferenceEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/target.md", "type: note\ntitle: Target\n", "")
	writeNode(test, root, "notes/source.md", "type: note\ntitle: Source\n", "Refer to [[notes/target]].\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
			Wikilinks:   true,
		},
	}

	if _, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo, Edges: edgeRepo, EdgeTypes: edgeTypes}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	listed, _ := edgeRepo.ListBySource("notes/source")

	if len(listed) != 1 || listed[0].Type != "references" || listed[0].TargetID != "notes/target" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestRun_RemovedFileAlsoRemovesEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "tickets/a.md", "type: ticket\ntitle: A\nparent: tickets/b\n", "")
	writeNode(test, root, "tickets/b.md", "type: ticket\ntitle: B\n", "")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToOne,
		},
	}

	cfg := reindex.Config{Root: root, Repo: repo, Edges: edgeRepo, EdgeTypes: edgeTypes}

	if _, runErr := reindex.Run(cfg); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	if rmErr := os.Remove(filepath.Join(root, "tickets/a.md")); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	if _, runErr := reindex.Run(cfg); runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	listed, _ := edgeRepo.ListBySource("tickets/a")

	if len(listed) != 0 {
		test.Errorf("expected zero edges after node removal, got %+v", listed)
	}
}

func TestRun_RespectsRootGitignore(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n*.tmp\n"), 0o644); writeErr != nil {
		test.Fatalf("gitignore: %v", writeErr)
	}

	writeNode(test, root, "real.md", "type: note\n", "Body.\n")
	writeNode(test, root, "build/internal.md", "type: note\n", "Body.\n")
	writeNode(test, root, "scratch.tmp", "type: note\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)

	report, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 1 {
		test.Errorf("Indexed = %d, want 1 (only real.md)", report.Indexed)
	}
}

func TestRun_RespectsWorkspaceIgnore(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "keep.md", "type: note\n", "")
	writeNode(test, root, "drafts/private.md", "type: note\n", "")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)

	report, runErr := reindex.Run(reindex.Config{
		Root:            root,
		Repo:            repo,
		WorkspaceIgnore: []string{"drafts/"},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 1 {
		test.Errorf("Indexed = %d, want 1 (drafts/ excluded by workspace ignore)", report.Indexed)
	}
}

type stubEmbedder struct {
	model string
	dim   int
	calls int
}

func (stub *stubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.calls++

	return make([]float32, stub.dim), nil
}

func (stub *stubEmbedder) Model() string { return stub.model }
func (stub *stubEmbedder) Dim() int      { return stub.dim }

func TestRun_DrainsEmbedQueue(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "a.md", "type: note\ntitle: A\n", "Body.\n")
	writeNode(test, root, "b.md", "type: note\ntitle: B\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)
	embedder := &stubEmbedder{model: "test", dim: 3}

	cfg := reindex.Config{
		Root:          root,
		Repo:          nodeRepo,
		Edges:         edgeRepo,
		EdgeTypes:     manifest.EdgeTypes{},
		EmbedQueue:    queueRepo,
		EmbeddingRepo: embeddingRepo,
		Embedder:      embedder,
		Chunker:       embed.WholeDocument{},
	}

	report, runErr := reindex.Run(cfg)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	if embedder.calls != 2 {
		test.Errorf("embedder calls = %d, want 2", embedder.calls)
	}

	depth, _ := queueRepo.Depth()

	if depth != 0 {
		test.Errorf("queue depth = %d, want 0 after drain", depth)
	}

	loaded, _ := embeddingRepo.GetByNodeID("a")

	if len(loaded) != 1 || loaded[0].Dim != 3 {
		test.Errorf("embedding for a = %+v", loaded)
	}
}

func TestRun_RecordsLastReindexAt(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	metaRepo := index.NewMetaRepo(store)

	if _, runErr := reindex.Run(reindex.Config{
		Root: root,
		Repo: index.NewNodeRepo(store),
		Meta: metaRepo,
	}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	stored, getErr := metaRepo.Get("last_reindex_at")

	if getErr != nil {
		test.Fatalf("meta Get: %v", getErr)
	}

	if stored == "" {
		test.Errorf("expected last_reindex_at to be set")
	}
}

func TestRun_OffSchemaStatusProducesDriftRow(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	// Write a ticket with off-schema status.
	if writeErr := os.WriteFile(filepath.Join(root, "ticket.md"), []byte(`---
type: ticket
status: bogus
---
body
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	driftRepo := index.NewWorkflowDriftRepo(store)
	engine := buildWorkflowEngineForReindexTest(test)

	report, runErr := reindex.Run(reindex.Config{
		Root:      root,
		Repo:      index.NewNodeRepo(store),
		Behaviors: engine,
		DriftLog:  driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.WorkflowViolations != 1 {
		test.Errorf("WorkflowViolations = %d, want 1", report.WorkflowViolations)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 || rows[0].ObservedStatus != "bogus" {
		test.Errorf("drift rows = %+v, want one row for 'bogus'", rows)
	}

	// Indexing still upserted the row.
	if _, getErr := index.NewNodeRepo(store).Get("ticket"); getErr != nil {
		test.Errorf("Get: %v (reindex should still upsert despite drift)", getErr)
	}
}

func TestRun_CleanPassClearsDrift(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, _ := index.Open(dbPath)

	defer store.Close()

	driftRepo := index.NewWorkflowDriftRepo(store)

	// Seed a stale drift row.
	if appendErr := driftRepo.Append(index.WorkflowDriftRow{
		NodeID: "ticket", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "ancient", Property: "status", ObservedAt: 1,
	}); appendErr != nil {
		test.Fatalf("seed Append: %v", appendErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "ticket.md"), []byte(`---
type: ticket
status: pending
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	engine := buildWorkflowEngineForReindexTest(test)

	if _, runErr := reindex.Run(reindex.Config{
		Root:      root,
		Repo:      index.NewNodeRepo(store),
		Behaviors: engine,
		DriftLog:  driftRepo,
	}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 0 {
		test.Errorf("drift after clean reindex = %+v, want empty", rows)
	}
}

func buildWorkflowEngineForReindexTest(test *testing.T) *behavior.Engine {
	test.Helper()

	const sample = `
[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`

	var decoded struct {
		Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`
	}

	meta, decodeErr := toml.Decode(sample, &decoded)

	if decodeErr != nil {
		test.Fatalf("toml decode: %v", decodeErr)
	}

	primitive := decoded.Behaviors["workflow"]["tickets"]

	instance, newErr := workflow.Kind{}.NewInstance("tickets", primitive, &meta)

	if newErr != nil {
		test.Fatalf("workflow.NewInstance: %v", newErr)
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{instance}, nil)

	return engine
}

func TestRun_OffSchemaPropertyProducesDriftRow(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	if writeErr := os.WriteFile(filepath.Join(root, "ticket.md"), []byte(`---
type: ticket
priority: high
---
body
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "priority", Type: "int"}}},
	}

	driftRepo := index.NewPropertyDriftRepo(store)

	report, runErr := reindex.Run(reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		NodeTypes:     decls,
		PropertyDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.PropertyViolations != 1 {
		test.Errorf("PropertyViolations = %d, want 1", report.PropertyViolations)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 || rows[0].Kind != "type-mismatch" {
		test.Errorf("drift rows = %+v", rows)
	}

	// Indexing still upserted the row.
	if _, getErr := index.NewNodeRepo(store).Get("ticket"); getErr != nil {
		test.Errorf("Get: %v (reindex should still upsert despite drift)", getErr)
	}
}

func TestRun_CleanPassClearsPropertyDrift(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, _ := index.Open(dbPath)

	defer store.Close()

	driftRepo := index.NewPropertyDriftRepo(store)

	if appendErr := driftRepo.Append(index.PropertyDriftRow{
		NodeID: "ticket", NodeType: "ticket", Kind: "type-mismatch", Property: "priority", ObservedAt: 1,
	}); appendErr != nil {
		test.Fatalf("seed Append: %v", appendErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "ticket.md"), []byte(`---
type: ticket
priority: 3
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "priority", Type: "int"}}},
	}

	if _, runErr := reindex.Run(reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		NodeTypes:     decls,
		PropertyDrift: driftRepo,
	}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 0 {
		test.Errorf("drift after clean reindex = %+v, want empty", rows)
	}
}

func TestReindex_RefDanglingProducesDrift(test *testing.T) {
	dir := test.TempDir()

	manifestContent := `
[workspace]
name = "test"

[node-types.person]
properties = [
    { name = "name", type = "string", required = true },
]

[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
]
`

	if writeErr := os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(manifestContent), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(dir, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(dir, "tickets/auth.md"), []byte(
		"---\ntype: ticket\ntitle: Auth\nassignee: missing\n---\n\nbody\n",
	), 0o644); writeErr != nil {
		test.Fatalf("write ticket: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(dir, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	idx, idxErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if idxErr != nil {
		test.Fatalf("open index: %v", idxErr)
	}

	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)
	loaded, loadErr := manifest.Load(filepath.Join(dir, "tusk.toml"))

	if loadErr != nil {
		test.Fatalf("Load manifest: %v", loadErr)
	}

	report, runErr := reindex.Run(reindex.Config{
		Root:          dir,
		Repo:          index.NewNodeRepo(idx),
		Edges:         index.NewEdgeRepo(idx),
		EdgeTypes:     loaded.EdgeTypes,
		NodeTypes:     loaded.NodeTypes,
		PropertyDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.RefDangling < 1 {
		test.Errorf("RefDangling = %d, want >= 1", report.RefDangling)
	}

	rows, _ := driftRepo.ListAll()

	var found bool

	for _, row := range rows {
		if row.Kind == doctor.IssueRefDangling && row.Property == "assignee" {
			found = true
		}
	}

	if !found {
		test.Errorf("expected ref_dangling row for assignee, got %+v", rows)
	}
}

func TestReindex_RefCleanPassClearsDrift(test *testing.T) {
	dir := test.TempDir()

	manifestContent := `
[workspace]
name = "test"

[node-types.person]
properties = [
    { name = "name", type = "string", required = true },
]

[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
]
`

	if writeErr := os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(manifestContent), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(dir, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir tickets: %v", mkErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(dir, "people"), 0o755); mkErr != nil {
		test.Fatalf("mkdir people: %v", mkErr)
	}

	// Write a person node (alice) and a ticket referencing her.
	if writeErr := os.WriteFile(filepath.Join(dir, "people/alice.md"), []byte(
		"---\ntype: person\ntitle: alice\nname: Alice\n---\n\nbio\n",
	), 0o644); writeErr != nil {
		test.Fatalf("write person: %v", writeErr)
	}

	if writeErr := os.WriteFile(filepath.Join(dir, "tickets/auth.md"), []byte(
		"---\ntype: ticket\ntitle: Auth\nassignee: alice\n---\n\nbody\n",
	), 0o644); writeErr != nil {
		test.Fatalf("write ticket: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(dir, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	idx, idxErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if idxErr != nil {
		test.Fatalf("open index: %v", idxErr)
	}

	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)
	loaded, loadErr := manifest.Load(filepath.Join(dir, "tusk.toml"))

	if loadErr != nil {
		test.Fatalf("Load manifest: %v", loadErr)
	}

	// Pre-seed a stale ref_dangling row for tickets/auth.
	if appendErr := driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/auth",
		NodeType: "ticket",
		Kind:     doctor.IssueRefDangling,
		Property: "assignee",
		Details:  `{"value":"old-missing","to":"person"}`,
	}); appendErr != nil {
		test.Fatalf("append stale row: %v", appendErr)
	}

	_, runErr := reindex.Run(reindex.Config{
		Root:          dir,
		Repo:          index.NewNodeRepo(idx),
		Edges:         index.NewEdgeRepo(idx),
		EdgeTypes:     loaded.EdgeTypes,
		NodeTypes:     loaded.NodeTypes,
		PropertyDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	rows, _ := driftRepo.ListAll()

	for _, row := range rows {
		if row.NodeID == "tickets/auth" {
			test.Errorf("stale drift row not cleared: %+v", row)
		}
	}
}

func TestRun_LogsWalkStartAndComplete(test *testing.T) {
	tempDir := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(tempDir, "a.md"), []byte("---\nid: a\ntype: note\n---\nbody"), 0o644); writeErr != nil {
		test.Fatalf("write a: %v", writeErr)
	}

	store, openErr := index.Open(filepath.Join(tempDir, "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	defer store.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, runErr := reindex.Run(reindex.Config{
		Root:   tempDir,
		Repo:   index.NewNodeRepo(store),
		Logger: logger,
	})

	if runErr != nil {
		test.Fatalf("run: %v", runErr)
	}

	out := buf.String()

	if !strings.Contains(out, `msg="reindex walk start"`) {
		test.Errorf("expected walk-start log; got %q", out)
	}

	if !strings.Contains(out, `msg="reindex walk complete"`) {
		test.Errorf("expected walk-complete log; got %q", out)
	}

	if !strings.Contains(out, "indexed=1") {
		test.Errorf("expected indexed=1; got %q", out)
	}
}

func TestRun_UnflaggedReferencesEdgeDoesNotMaterialize(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/target.md", "type: note\ntitle: Target\n", "")
	writeNode(test, root, "notes/source.md", "type: note\ntitle: Source\n", "Refer to [[notes/target]].\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
			// no Wikilinks flag
		},
	}

	if _, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo, Edges: edgeRepo, EdgeTypes: edgeTypes}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	listed, listErr := edgeRepo.ListBySource("notes/source")

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	if len(listed) != 0 {
		test.Errorf("listed = %+v, want no edges (references unflagged)", listed)
	}
}

// TestRun_SubUnitsEnabled_WritesAndConvergesRows is the end-to-end
// happy-path test for the sub-unit pipeline (Phase 2 Task 3). It seeds
// three markdown files exercising headings, lists, wikilinks, and a
// degenerate single-paragraph body, runs Reindex with sub-units
// enabled, then rewrites one paragraph in one file and re-runs to
// confirm only the edited paragraph's row id changes.
func TestRun_SubUnitsEnabled_WritesAndConvergesRows(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/long.md", "type: note\ntitle: Long\n",
		"# Heading\n\nFirst paragraph.\n\n## Sub\n\n- list item one\n- list item two\n")
	writeNode(test, root, "notes/wikilink.md", "type: note\ntitle: WL\n",
		"see [[notes/long]] for context\n")
	writeNode(test, root, "notes/tiny.md", "type: note\ntitle: Tiny\n", "just one paragraph\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
			Wikilinks:   true,
		},
	}

	// Hand-built manifest with no Meta — SubUnitsEnabled() returns
	// true by default for hand-built manifests.
	loaded := &manifest.Manifest{EdgeTypes: edgeTypes}

	cfg := reindex.Config{
		Root:      root,
		Repo:      repo,
		Edges:     edgeRepo,
		EdgeTypes: edgeTypes,
		Manifest:  loaded,
	}

	report, runErr := reindex.Run(cfg)

	if runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	if report.SubUnitsInserted == 0 {
		test.Errorf("SubUnitsInserted = 0, want >0")
	}

	longRows, listErr := repo.ListSubUnitsForFile("notes/long")

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	if len(longRows) == 0 {
		test.Errorf("expected sub-unit rows for notes/long, got none")
	}

	tinyRows, _ := repo.ListSubUnitsForFile("notes/tiny")

	if len(tinyRows) != 1 {
		test.Errorf("tiny sub-unit rows = %d, want 1", len(tinyRows))
	}

	// A wikilink in a sub-unit body must materialize as a references
	// edge with the sub-unit row as the source.
	wikiRows, _ := repo.ListSubUnitsForFile("notes/wikilink")

	if len(wikiRows) == 0 {
		test.Fatalf("no sub-unit rows for wikilink file")
	}

	var sawWikilinkEdge bool

	for _, row := range wikiRows {
		listed, _ := edgeRepo.ListBySource(row.ID)

		for _, edge := range listed {
			if edge.Type == "references" && edge.TargetID == "notes/long" {
				sawWikilinkEdge = true
			}
		}
	}

	if !sawWikilinkEdge {
		test.Errorf("expected references edge from a wikilink sub-unit to notes/long")
	}

	// `contains` edges from the file row to each sub-unit (one per row).
	containsEdges, _ := edgeRepo.ListBySource("notes/long")

	var containsCount int

	for _, edge := range containsEdges {
		if edge.Type == "contains" {
			containsCount++
		}
	}

	if containsCount != len(longRows) {
		test.Errorf("contains edges = %d, want %d", containsCount, len(longRows))
	}

	// Capture pre-edit ids so we can prove only the edited row turns over.
	preTinyIDs := map[string]struct{}{}

	for _, row := range tinyRows {
		preTinyIDs[row.ID] = struct{}{}
	}

	preLongIDs := map[string]struct{}{}

	for _, row := range longRows {
		preLongIDs[row.ID] = struct{}{}
	}

	// Edit one paragraph in `notes/tiny.md` — the row id (a hash of
	// the body) must change; long and wikilink rows must not.
	writeNode(test, root, "notes/tiny.md", "type: note\ntitle: Tiny\n", "edited paragraph body\n")

	if _, runErr = reindex.Run(cfg); runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	postTinyRows, _ := repo.ListSubUnitsForFile("notes/tiny")

	if len(postTinyRows) != 1 {
		test.Errorf("post tiny rows = %d, want 1", len(postTinyRows))
	}

	if _, kept := preTinyIDs[postTinyRows[0].ID]; kept {
		test.Errorf("tiny row id %q unchanged after edit; expected new hash", postTinyRows[0].ID)
	}

	postLongRows, _ := repo.ListSubUnitsForFile("notes/long")

	if len(postLongRows) != len(longRows) {
		test.Errorf("long rows churned: pre=%d post=%d", len(longRows), len(postLongRows))
	}

	for _, row := range postLongRows {
		if _, kept := preLongIDs[row.ID]; !kept {
			test.Errorf("long row id %q changed across reindex passes", row.ID)
		}
	}
}

// TestRun_SubUnitsDisabled_NoSubUnitRows is the back-compat regression:
// when SubUnitsEnabled() is false, the reindex pass must NOT write any
// sub-unit rows. Loaded from a real manifest file so the toml.MetaData
// is present and the disable signal reaches the engine.
func TestRun_SubUnitsDisabled_NoSubUnitRows(test *testing.T) {
	root := test.TempDir()

	manifestPath := filepath.Join(root, "tusk.toml")

	manifestBody := `
[workspace]
name = "test"
sub-units = false

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
wikilinks = true
`

	if writeErr := os.WriteFile(manifestPath, []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	if loaded.SubUnitsEnabled() {
		test.Fatalf("manifest opted out but SubUnitsEnabled() = true")
	}

	writeNode(test, root, "notes/long.md", "type: note\ntitle: Long\n",
		"# H\n\nfirst\n\n- a\n- b\n")
	writeNode(test, root, "notes/tiny.md", "type: note\ntitle: T\n", "just one\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	report, runErr := reindex.Run(reindex.Config{
		Root:      root,
		Repo:      repo,
		Edges:     edgeRepo,
		EdgeTypes: loaded.EdgeTypes,
		Manifest:  loaded,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.SubUnitsInserted != 0 {
		test.Errorf("SubUnitsInserted = %d, want 0 with sub-units disabled", report.SubUnitsInserted)
	}

	for _, fileID := range []string{"notes/long", "notes/tiny"} {
		subRows, _ := repo.ListSubUnitsForFile(fileID)

		if len(subRows) != 0 {
			test.Errorf("%s: sub-unit rows present with sub-units disabled: %d", fileID, len(subRows))
		}
	}

	// Defense-in-depth: assert directly against the schema that no
	// sub-unit row exists. ListSubUnitsForFile filters by id prefix;
	// this query catches any row that slipped past the prefix filter
	// (e.g., if SubUnitsEnabled() ever changes its default semantics
	// and the engine starts writing sub-units under a different id
	// scheme).
	var subUnitCount int

	scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind = 'subunit'`).Scan(&subUnitCount)

	if scanErr != nil {
		test.Fatalf("count sub-unit rows: %v", scanErr)
	}

	if subUnitCount != 0 {
		test.Errorf("nodes with kind='subunit' = %d, want 0 with sub-units disabled", subUnitCount)
	}
}

func writeNode(test *testing.T, root, relPath, frontmatter, body string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	content := "---\n" + frontmatter + "---\n\n" + body

	if filepath.Ext(relPath) != ".md" {
		content = body
	}

	if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}
}
