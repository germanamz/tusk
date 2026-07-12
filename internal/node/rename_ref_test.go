package node_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// refRenameFixture wires a ref-aware service and edge/node repos over a fresh
// temp index. It is the shape every #680 rename test needs: a `person` type and
// a `task` type whose `assignee` ref-property auto-generates a derived edge.
type refRenameFixture struct {
	root      string
	nodeRepo  *index.NodeRepo
	edgeRepo  *index.EdgeRepo
	fileState *index.FileStateRepo
	service   *node.Service
	edgeTypes manifest.EdgeTypes
	nodeTypes map[string]manifest.NodeType
}

func newRefRenameFixture(test *testing.T) refRenameFixture {
	test.Helper()

	root := test.TempDir()
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { _ = store.Close() })

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)

	nodeTypes := map[string]manifest.NodeType{
		"person": {},
		"task":   {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}
	edgeTypes := manifest.EdgeTypes{
		"assignee": {From: []string{"task"}, To: []string{"person"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithBehaviors(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		nodeTypes, nil, nil, nil, nil,
		node.NewIndexRefLookup(nodeRepo),
		fileState, "test-worker", time.Minute,
	)

	return refRenameFixture{
		root:      root,
		nodeRepo:  nodeRepo,
		edgeRepo:  edgeRepo,
		fileState: fileState,
		service:   service,
		edgeTypes: edgeTypes,
		nodeTypes: nodeTypes,
	}
}

func (fixture refRenameFixture) assigneeTarget(test *testing.T, source string) string {
	test.Helper()

	rows, listErr := fixture.edgeRepo.ListBySource(source)

	if listErr != nil {
		test.Fatalf("list edges from %s: %v", source, listErr)
	}

	for _, row := range rows {
		if row.Type == "assignee" {
			return row.TargetID
		}
	}

	return ""
}

func (fixture refRenameFixture) rename(test *testing.T, oldID, newRelPath string) *node.RenamePlan {
	test.Helper()

	plan, renameErr := node.Rename(
		fixture.root, fixture.nodeRepo, fixture.edgeRepo, fixture.fileState,
		"test-worker", time.Minute, fixture.edgeTypes, fixture.nodeTypes, nil,
		oldID, newRelPath,
	)

	if renameErr != nil {
		test.Fatalf("Rename %s -> %s: %v", oldID, newRelPath, renameErr)
	}

	return plan
}

// #680 F1: a referrer whose ref is a bare title keeps its RESOLVED edge id
// after the target moves — the re-derive must run ResolveRefs, not persist the
// raw frontmatter title as the edge target.
func TestRename_PreservesResolvedRefEdgeForBareTitleReferrer(test *testing.T) {
	fixture := newRefRenameFixture(test)

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "people/jane.md", Type: "person", Title: "Jane Doe",
	}); createErr != nil {
		test.Fatalf("create person: %v", createErr)
	}

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "tasks/auth.md", Type: "task", Title: "Auth",
		Properties: map[string]any{"assignee": "Jane Doe"},
	}); createErr != nil {
		test.Fatalf("create task: %v", createErr)
	}

	if got := fixture.assigneeTarget(test, "tasks/auth"); got != "people/jane" {
		test.Fatalf("assignee edge before move = %q, want people/jane", got)
	}

	fixture.rename(test, "people/jane", "people/jane-doe.md")

	if got := fixture.assigneeTarget(test, "tasks/auth"); got != "people/jane-doe" {
		test.Errorf("assignee edge after move = %q, want people/jane-doe (raw title clobbered resolved id)", got)
	}
}

// #680 F2: a wikilink-form ref value in frontmatter is rewritten on disk AND in
// the index, so it keeps resolving after the target moves rather than becoming a
// permanent dead id.
func TestRename_RewritesWikilinkFormRefInFrontmatter(test *testing.T) {
	fixture := newRefRenameFixture(test)

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "people/jane.md", Type: "person", Title: "Jane Doe",
	}); createErr != nil {
		test.Fatalf("create person: %v", createErr)
	}

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "tasks/auth.md", Type: "task", Title: "Auth",
		Properties: map[string]any{"assignee": "[[people/jane]]"},
	}); createErr != nil {
		test.Fatalf("create task: %v", createErr)
	}

	if got := fixture.assigneeTarget(test, "tasks/auth"); got != "people/jane" {
		test.Fatalf("assignee edge before move = %q, want people/jane", got)
	}

	fixture.rename(test, "people/jane", "people/renamed.md")

	content, readErr := os.ReadFile(filepath.Join(fixture.root, "tasks/auth.md"))

	if readErr != nil {
		test.Fatalf("read task: %v", readErr)
	}

	if strings.Contains(string(content), "people/jane]]") {
		test.Errorf("frontmatter wikilink not rewritten on disk:\n%s", content)
	}

	if !strings.Contains(string(content), "people/renamed]]") {
		test.Errorf("frontmatter wikilink missing new id on disk:\n%s", content)
	}

	if got := fixture.assigneeTarget(test, "tasks/auth"); got != "people/renamed" {
		test.Errorf("assignee edge after move = %q, want people/renamed", got)
	}
}

// #692: an UNQUOTED frontmatter wikilink `assignee: [[people/jane]]` decodes to
// a nested YAML sequence, but the `[[` `]]` are the wikilink's own brackets. A
// move must rewrite only the inner id on disk (keeping the link unquoted) and
// the re-derive must re-resolve it — the whole point being that the unquoted
// form now resolves and stays in sync exactly like its quoted twin.
func TestRename_RewritesUnquotedWikilinkRefInFrontmatter(test *testing.T) {
	fixture := newRefRenameFixture(test)

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "people/jane.md", Type: "person", Title: "Jane Doe",
	}); createErr != nil {
		test.Fatalf("create person: %v", createErr)
	}

	// Create seeds the assignee edge in the index (via the quoted string form),
	// then overwrite the file on disk with the UNQUOTED wikilink a human would
	// hand-write — the form the rewriter and re-derive must handle.
	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "tasks/auth.md", Type: "task", Title: "Auth",
		Properties: map[string]any{"assignee": "[[people/jane]]"},
	}); createErr != nil {
		test.Fatalf("create task: %v", createErr)
	}

	taskPath := filepath.Join(fixture.root, "tasks/auth.md")
	unquoted := "---\ntype: task\ntitle: Auth\nassignee: [[people/jane]]\n---\n\nbody\n"

	if writeErr := os.WriteFile(taskPath, []byte(unquoted), 0o644); writeErr != nil {
		test.Fatalf("overwrite task with unquoted wikilink: %v", writeErr)
	}

	if got := fixture.assigneeTarget(test, "tasks/auth"); got != "people/jane" {
		test.Fatalf("assignee edge before move = %q, want people/jane", got)
	}

	fixture.rename(test, "people/jane", "people/renamed.md")

	content, readErr := os.ReadFile(taskPath)

	if readErr != nil {
		test.Fatalf("read task: %v", readErr)
	}

	// The link stays unquoted with brackets intact, retargeted to the new id.
	if !strings.Contains(string(content), "assignee: [[people/renamed]]") {
		test.Errorf("unquoted wikilink not retargeted in place on disk:\n%s", content)
	}

	if strings.Contains(string(content), "people/jane]]") {
		test.Errorf("old id still present on disk:\n%s", content)
	}

	if got := fixture.assigneeTarget(test, "tasks/auth"); got != "people/renamed" {
		test.Errorf("assignee edge after move = %q, want people/renamed", got)
	}
}

// #680 F3: a chained move A->B->C keeps tracking a bare-title referrer at every
// hop — the first move must not corrupt the edge target and drop the referrer
// from the second move's affected set.
func TestRename_ChainedMovePreservesReferrerTracking(test *testing.T) {
	fixture := newRefRenameFixture(test)

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "people/a.md", Type: "person", Title: "Amy Chan",
	}); createErr != nil {
		test.Fatalf("create person: %v", createErr)
	}

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "tasks/chain.md", Type: "task", Title: "Rotate keys",
		Properties: map[string]any{"assignee": "Amy Chan"},
	}); createErr != nil {
		test.Fatalf("create task: %v", createErr)
	}

	fixture.rename(test, "people/a", "people/b.md")

	if got := fixture.assigneeTarget(test, "tasks/chain"); got != "people/b" {
		test.Fatalf("assignee edge after first move = %q, want people/b", got)
	}

	plan := fixture.rename(test, "people/b", "people/c.md")

	if len(plan.AffectedFiles) != 1 {
		test.Errorf("second move affected files = %v, want the referrer tracked (len 1)", plan.AffectedFiles)
	}

	if got := fixture.assigneeTarget(test, "tasks/chain"); got != "people/c" {
		test.Errorf("assignee edge after second move = %q, want people/c (referrer tracking lost)", got)
	}
}

// #680 F5: a referrer's structural `contains` edges survive the move. The
// re-derive replaces only the referrer's file-level content edges; its sub-unit
// contains rows must not be wiped (they are re-created only by a re-parse, which
// a byte-unchanged referrer never gets on a plain reindex).
func TestRename_PreservesReferrerStructuralEdges(test *testing.T) {
	fixture := newRefRenameFixture(test)

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "people/jane.md", Type: "person", Title: "Jane Doe",
	}); createErr != nil {
		test.Fatalf("create person: %v", createErr)
	}

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "tasks/auth.md", Type: "task", Title: "Auth",
		Properties: map[string]any{"assignee": "Jane Doe"},
		Body:       []byte("## Section one\n\nBody.\n"),
	}); createErr != nil {
		test.Fatalf("create task: %v", createErr)
	}

	// Seed the referrer's structural contains edge the way the sub-unit
	// pipeline would; Service.Create does not run sub-unit sync.
	if upsertErr := fixture.edgeRepo.InsertIgnore([]index.EdgeRow{{
		Type: "contains", SourceID: "tasks/auth", TargetID: "tasks/auth#S1",
		SourcePath: "tasks/auth.md", Kind: "structural",
		Source: sql.NullString{String: "markdown", Valid: true},
	}}); upsertErr != nil {
		test.Fatalf("seed structural edge: %v", upsertErr)
	}

	fixture.rename(test, "people/jane", "people/jane-doe.md")

	rows, listErr := fixture.edgeRepo.ListBySource("tasks/auth")

	if listErr != nil {
		test.Fatalf("list edges: %v", listErr)
	}

	var (
		haveStructural bool
		haveAssignee   bool
	)

	for _, row := range rows {
		if row.Kind == "structural" && row.TargetID == "tasks/auth#S1" {
			haveStructural = true
		}

		if row.Type == "assignee" && row.TargetID == "people/jane-doe" {
			haveAssignee = true
		}
	}

	if !haveStructural {
		test.Errorf("referrer lost its structural contains edge after move: %+v", rows)
	}

	if !haveAssignee {
		test.Errorf("referrer assignee edge = %+v, want target people/jane-doe", rows)
	}
}

// #680: when the move re-derives a referrer that also carries a pre-existing
// broken ref, that ref is dropped from the index AND recorded as ref drift, so
// it stays visible to `tusk doctor` and the reindex heal pass rather than
// silently vanishing.
func TestRename_RecordsRefDriftForBrokenReferrerRef(test *testing.T) {
	root := test.TempDir()
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)
	driftRepo := index.NewPropertyDriftRepo(store)

	nodeTypes := map[string]manifest.NodeType{
		"person": {},
		"task": {Properties: []manifest.PropertyDecl{
			{Name: "assignee", Type: "ref", To: "person"},
			{Name: "reviewer", Type: "ref", To: "person"},
		}},
	}
	edgeTypes := manifest.EdgeTypes{
		"assignee": {From: []string{"task"}, To: []string{"person"}, Cardinality: manifest.CardinalityManyToOne},
		"reviewer": {From: []string{"task"}, To: []string{"person"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithBehaviors(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		nodeTypes, driftRepo, nil, nil, nil,
		node.NewIndexRefLookup(nodeRepo),
		fileState, "test-worker", time.Minute,
	)

	for _, person := range []struct{ path, title string }{
		{"people/jane.md", "Jane Doe"},
		{"people/ghost.md", "Ghost Reviewer"},
	} {
		if _, createErr := service.Create(node.CreateInput{RelPath: person.path, Type: "person", Title: person.title}); createErr != nil {
			test.Fatalf("create %s: %v", person.path, createErr)
		}
	}

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "tasks/auth.md", Type: "task", Title: "Auth",
		Properties: map[string]any{"assignee": "Jane Doe", "reviewer": "Ghost Reviewer"},
	}); createErr != nil {
		test.Fatalf("create task: %v", createErr)
	}

	// Delete the reviewer target out from under the referrer: node.Delete
	// leaves the incoming edge dangling and records NO drift, so the broken
	// reviewer ref is exactly the pre-existing-but-undriften state the move
	// must not silently swallow.
	if deleteErr := node.Delete(root, nodeRepo, edgeRepo, fileState, nil, "test-worker", time.Minute, "people/ghost"); deleteErr != nil {
		test.Fatalf("delete ghost: %v", deleteErr)
	}

	if _, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "test-worker", time.Minute,
		edgeTypes, nodeTypes, driftRepo, "people/jane", "people/jane-doe.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	driftRows, listErr := driftRepo.ListAll()

	if listErr != nil {
		test.Fatalf("list drift: %v", listErr)
	}

	haveReviewerDrift := false

	for _, row := range driftRows {
		if row.NodeID == "tasks/auth" && row.Property == "reviewer" && row.Kind == string(node.RefErrDangling) {
			haveReviewerDrift = true
		}
	}

	if !haveReviewerDrift {
		test.Errorf("no ref_dangling drift recorded for tasks/auth reviewer after move: %+v", driftRows)
	}
}

// #680 review: a bare ref value resolves by TITLE, which a move never changes,
// so a bare value that merely coincides with the moved node's OLD id must be
// left untouched on disk — otherwise the reference (still valid by title) is
// rewritten into a broken one. Regression for the sole confirmed review finding.
// TestDelete_EnqueuesDerivedReferrers pins #689 finding 1 for the direct
// `tusk node delete` path: deleting a ref target must enqueue the files whose
// derived edge pointed at it, so a later reindex re-resolves them instead of
// leaving the edge frozen at the dead id until `reindex --force`.
func TestDelete_EnqueuesDerivedReferrers(test *testing.T) {
	root := test.TempDir()
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)
	queue := index.NewEmbedQueueRepo(store)

	nodeTypes := map[string]manifest.NodeType{
		"person": {},
		"task": {Properties: []manifest.PropertyDecl{
			{Name: "assignee", Type: "ref", To: "person"},
		}},
	}
	edgeTypes := manifest.EdgeTypes{
		"assignee": {From: []string{"task"}, To: []string{"person"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithBehaviors(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		nodeTypes, index.NewPropertyDriftRepo(store), nil, nil, nil,
		node.NewIndexRefLookup(nodeRepo),
		fileState, "test-worker", time.Minute,
	)

	if _, createErr := service.Create(node.CreateInput{RelPath: "people/jane.md", Type: "person", Title: "Jane Doe"}); createErr != nil {
		test.Fatalf("create person: %v", createErr)
	}

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "tasks/auth.md", Type: "task", Title: "Auth",
		Properties: map[string]any{"assignee": "Jane Doe"},
	}); createErr != nil {
		test.Fatalf("create task: %v", createErr)
	}

	if edges, _ := edgeRepo.ListByTarget("people/jane"); len(edges) != 1 || edges[0].SourceID != "tasks/auth" || edges[0].Kind != "derived" {
		test.Fatalf("precondition: want derived assignee edge tasks/auth -> people/jane, got %+v", edges)
	}

	if deleteErr := node.Delete(root, nodeRepo, edgeRepo, fileState, queue, "test-worker", time.Minute, "people/jane"); deleteErr != nil {
		test.Fatalf("delete: %v", deleteErr)
	}

	rows, queryErr := store.DB().Query(`SELECT node_id FROM embed_queue WHERE kind = 'reindex'`)

	if queryErr != nil {
		test.Fatalf("query embed_queue: %v", queryErr)
	}

	defer rows.Close()

	enqueued := map[string]bool{}

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}

		enqueued[id] = true
	}

	if !enqueued["reindex:tasks/auth.md"] {
		test.Errorf("node delete must enqueue the derived referrer for re-resolution; queue = %v", enqueued)
	}
}

func TestRename_LeavesBareTitleRefWhenTitleEqualsOldID(test *testing.T) {
	root := test.TempDir()
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)

	nodeTypes := map[string]manifest.NodeType{
		"note": {Properties: []manifest.PropertyDecl{{Name: "parent", Type: "ref", To: "note"}}},
	}
	edgeTypes := manifest.EdgeTypes{
		"parent": {From: []string{"note"}, To: []string{"note"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithBehaviors(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		nodeTypes, nil, nil, nil, nil,
		node.NewIndexRefLookup(nodeRepo),
		fileState, "test-worker", time.Minute,
	)

	// notes/foo has title "foo" — its title string equals its node id "foo".
	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/foo.md", Type: "note", Title: "foo"}); createErr != nil {
		test.Fatalf("create foo: %v", createErr)
	}

	// notes/child references it by bare title "foo" (resolves via FindByTitle).
	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/child.md", Type: "note", Title: "Child",
		Properties: map[string]any{"parent": "foo"},
	}); createErr != nil {
		test.Fatalf("create child: %v", createErr)
	}

	parentTarget := func() string {
		rows, _ := edgeRepo.ListBySource("notes/child")

		for _, row := range rows {
			if row.Type == "parent" {
				return row.TargetID
			}
		}

		return ""
	}

	if got := parentTarget(); got != "notes/foo" {
		test.Fatalf("parent edge before move = %q, want notes/foo", got)
	}

	if _, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "test-worker", time.Minute,
		edgeTypes, nodeTypes, nil, "notes/foo", "notes/bar.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	// The bare title "foo" must be left on disk untouched (title is stable).
	childContent, readErr := os.ReadFile(filepath.Join(root, "notes/child.md"))

	if readErr != nil {
		test.Fatalf("read child: %v", readErr)
	}

	if !strings.Contains(string(childContent), "parent: foo") {
		test.Errorf("bare title ref was rewritten; want it left as 'parent: foo':\n%s", childContent)
	}

	// And the edge keeps resolving to the node at its new id.
	if got := parentTarget(); got != "notes/bar" {
		test.Errorf("parent edge after move = %q, want notes/bar (title 'foo' still resolves)", got)
	}
}
