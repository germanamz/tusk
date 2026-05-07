package node_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func openTempIndex(test *testing.T, root string) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	return store
}

func newTestService(test *testing.T) (*node.Service, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	service := node.NewService(root, index.NewNodeRepo(store))

	return service, root
}

func TestService_CreateWritesFileAndIndexes(test *testing.T) {
	service, root := newTestService(test)

	created, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/fix-login.md",
		Type:    "ticket",
		Title:   "Fix login",
		Body:    []byte("Some body.\n"),
	})

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if created.ID != "tickets/fix-login" {
		test.Errorf("ID = %q", created.ID)
	}

	onDisk, readErr := os.ReadFile(filepath.Join(root, "tickets/fix-login.md"))

	if readErr != nil {
		test.Fatalf("read file: %v", readErr)
	}

	if !contains(string(onDisk), "type: ticket") {
		test.Errorf("file missing type: %s", string(onDisk))
	}

	loaded, getErr := service.Get("tickets/fix-login")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if loaded.Title != "Fix login" {
		test.Errorf("Title = %q", loaded.Title)
	}
}

func TestService_CreateRejectsExistingFile(test *testing.T) {
	service, _ := newTestService(test)

	if _, firstErr := service.Create(node.CreateInput{
		RelPath: "x.md", Type: "note", Body: []byte(""),
	}); firstErr != nil {
		test.Fatalf("first Create: %v", firstErr)
	}

	_, secondErr := service.Create(node.CreateInput{
		RelPath: "x.md", Type: "note", Body: []byte(""),
	})

	if secondErr != node.ErrAlreadyExists {
		test.Errorf("err = %v, want ErrAlreadyExists", secondErr)
	}
}

func TestService_ListReturnsAllNodes(test *testing.T) {
	service, _ := newTestService(test)

	service.Create(node.CreateInput{RelPath: "a.md", Type: "note", Body: []byte("")})
	service.Create(node.CreateInput{RelPath: "b.md", Type: "ticket", Body: []byte("")})

	all, listErr := service.List(node.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(all) != 2 {
		test.Errorf("len = %d, want 2", len(all))
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for offset := 0; offset+len(needle) <= len(haystack); offset++ {
		if haystack[offset:offset+len(needle)] == needle {
			return offset
		}
	}

	return -1
}

func newTestServiceWithManifest(test *testing.T, edgeTypes manifest.EdgeTypes) (*node.Service, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	service := node.NewServiceWithManifest(root, index.NewNodeRepo(store), index.NewEdgeRepo(store), edgeTypes)

	return service, root
}

func plan2EdgeRegistry() manifest.EdgeTypes {
	return manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket", "project"},
			Cardinality: manifest.CardinalityManyToOne,
		},
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
			Acyclic:     true,
		},
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}
}

func TestService_CreatePersistsFrontmatterEdges(test *testing.T) {
	service, _ := newTestServiceWithManifest(test, plan2EdgeRegistry())

	parentInput := node.CreateInput{
		RelPath: "tickets/epic.md",
		Type:    "ticket",
		Title:   "Epic",
	}

	if _, parentErr := service.Create(parentInput); parentErr != nil {
		test.Fatalf("Create epic: %v", parentErr)
	}

	childInput := node.CreateInput{
		RelPath: "tickets/child.md",
		Type:    "ticket",
		Title:   "Child",
		Properties: map[string]any{
			"parent": "tickets/epic",
		},
	}

	created, createErr := service.Create(childInput)

	if createErr != nil {
		test.Fatalf("Create child: %v", createErr)
	}

	if !reflect.DeepEqual(created.Edges["parent"], []string{"tickets/epic"}) {
		test.Errorf("Edges[parent] = %v", created.Edges["parent"])
	}
}

func TestService_CreateRejectsIllegalEdgeSource(test *testing.T) {
	service, _ := newTestServiceWithManifest(test, plan2EdgeRegistry())

	if _, parentErr := service.Create(node.CreateInput{
		RelPath: "tickets/epic.md", Type: "ticket", Title: "Epic",
	}); parentErr != nil {
		test.Fatalf("Create epic: %v", parentErr)
	}

	noteInput := node.CreateInput{
		RelPath: "notes/bad.md",
		Type:    "note",
		Properties: map[string]any{
			"parent": "tickets/epic",
		},
	}

	_, createErr := service.Create(noteInput)

	if createErr == nil {
		test.Fatalf("expected error for source type mismatch")
	}
}

func TestService_CreateMaterializesWikilinksAsReferencesEdges(test *testing.T) {
	service, _ := newTestServiceWithManifest(test, plan2EdgeRegistry())

	if _, targetErr := service.Create(node.CreateInput{
		RelPath: "notes/target.md", Type: "note", Title: "Target",
	}); targetErr != nil {
		test.Fatalf("Create target: %v", targetErr)
	}

	created, createErr := service.Create(node.CreateInput{
		RelPath: "notes/source.md",
		Type:    "note",
		Title:   "Source",
		Body:    []byte("See [[notes/target]] for context.\n"),
	})

	if createErr != nil {
		test.Fatalf("Create source: %v", createErr)
	}

	wantTargets := []string{"notes/target"}

	if !reflect.DeepEqual(created.Edges["references"], wantTargets) {
		test.Errorf("Edges[references] = %v, want %v", created.Edges["references"], wantTargets)
	}
}

func TestService_CreateRejectsBlocksCycle(test *testing.T) {
	service, _ := newTestServiceWithManifest(test, plan2EdgeRegistry())

	_, selfErr := service.Create(node.CreateInput{
		RelPath:    "tickets/self.md",
		Type:       "ticket",
		Properties: map[string]any{"blocks": []any{"tickets/self"}},
	})

	if selfErr == nil {
		test.Fatalf("expected cycle error for self-blocks")
	}
}

func TestService_CreateEnqueuesEmbedding(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)

	service := node.NewServiceWithEmbedQueue(root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, queueRepo)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "n.md", Type: "note", Title: "N",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	depth, _ := queueRepo.Depth()

	if depth != 1 {
		test.Errorf("queue depth = %d, want 1", depth)
	}
}

func TestService_Modify_UpdatesProperty(test *testing.T) {
	root := test.TempDir()
	store := openTempIndex(test, root)
	defer store.Close()

	service := node.NewServiceWithManifest(root, index.NewNodeRepo(store), index.NewEdgeRepo(store), manifest.EdgeTypes{})

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/hi.md",
		Type:    "note",
		Title:   "Hi",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	modified, modifyErr := service.Modify(node.ModifyInput{
		ID:        "notes/hi",
		SetProps:  map[string]any{"priority": 5},
		UnsetKeys: nil,
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if modified.Properties["priority"] != 5 {
		test.Errorf("priority = %v, want 5", modified.Properties["priority"])
	}

	contents, _ := os.ReadFile(filepath.Join(root, "notes/hi.md"))

	if !strings.Contains(string(contents), "priority: 5") {
		test.Errorf("file should contain priority: 5\n%s", contents)
	}
}

func TestService_Modify_UnsetRemovesProperty(test *testing.T) {
	root := test.TempDir()
	store := openTempIndex(test, root)
	defer store.Close()

	service := node.NewServiceWithManifest(root, index.NewNodeRepo(store), index.NewEdgeRepo(store), manifest.EdgeTypes{})

	if _, createErr := service.Create(node.CreateInput{
		RelPath:    "notes/hi.md",
		Type:       "note",
		Properties: map[string]any{"priority": 5},
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	modified, modifyErr := service.Modify(node.ModifyInput{
		ID:        "notes/hi",
		UnsetKeys: []string{"priority"},
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if _, hasPriority := modified.Properties["priority"]; hasPriority {
		test.Errorf("priority should be unset")
	}
}

func TestService_Modify_RejectsTypeChange(test *testing.T) {
	root := test.TempDir()
	store := openTempIndex(test, root)
	defer store.Close()

	service := node.NewServiceWithManifest(root, index.NewNodeRepo(store), index.NewEdgeRepo(store), manifest.EdgeTypes{})

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/hi.md",
		Type:    "note",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	_, modifyErr := service.Modify(node.ModifyInput{
		ID:       "notes/hi",
		SetProps: map[string]any{"type": "ticket"},
	})

	if modifyErr == nil {
		test.Fatalf("expected error rejecting type change")
	}
}

func TestCreate_HookValidatePhaseRejectsBeforeWrite(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	// Build a behavior engine with a Validator that always rejects.
	rejector := &fakeServicePack{
		name: "rejector",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				return errors.New("denied")
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{rejector})

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		nil,
		nil,
		engine,
		index.NewWorkflowDriftRepo(store),
		io.Discard,
	)

	_, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
	})

	if createErr == nil || !strings.Contains(createErr.Error(), "denied") {
		test.Errorf("Create: expected rejection, got %v", createErr)
	}

	// File must not exist (rejection happens before write).
	if _, statErr := os.Stat(filepath.Join(root, "tickets/foo.md")); !os.IsNotExist(statErr) {
		test.Errorf("file present after rejection; statErr = %v", statErr)
	}
}

func TestCreate_HookAfterPhaseFiresAfterCommit(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	var afterCalled int

	tracker := &fakeServicePack{
		name: "tracker",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteAfter: func(ctx behavior.HookContext, before, after *node.Node) error {
				afterCalled++
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{tracker})

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		nil,
		nil,
		engine,
		index.NewWorkflowDriftRepo(store),
		io.Discard,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if afterCalled != 1 {
		test.Errorf("OnNodeWriteAfter: called %d times, want 1", afterCalled)
	}
}

// fakeServicePack mirrors the engine-package fake but lives in the
// node-package tests so we don't import test files across packages.
type fakeServicePack struct {
	name     string
	kind     string
	hooks    behavior.Hooks
	reserved []behavior.ReservedKey
}

func (pack *fakeServicePack) Name() string                         { return pack.name }
func (pack *fakeServicePack) Kind() string                         { return pack.kind }
func (pack *fakeServicePack) Hooks() behavior.Hooks                { return pack.hooks }
func (pack *fakeServicePack) ReservedKeys() []behavior.ReservedKey { return pack.reserved }

func TestModify_HookValidatePhaseRejects(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	// Pre-create a node without behaviors so the seed write succeeds.
	seed := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		nil, nil, nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"status": "active"},
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	// Build a Modify-time service with a rejecting validator.
	rejector := &fakeServicePack{
		name: "rejector",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				return errors.New("denied")
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{rejector})

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		nil,
		nil,
		engine,
		index.NewWorkflowDriftRepo(store),
		io.Discard,
	)

	_, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/foo",
		SetProps: map[string]any{"status": "done"},
	})

	if modifyErr == nil || !strings.Contains(modifyErr.Error(), "denied") {
		test.Errorf("Modify: expected rejection, got %v", modifyErr)
	}
}

func TestModify_HookRecoveryWritesDriftAndWarns(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	seed := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		nil, nil, nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"status": "blocked"}, // off-schema
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	// Build a workflow instance that doesn't know about "blocked".
	cfg := workflowConfigForTest(test)

	driftRepo := index.NewWorkflowDriftRepo(store)
	engine, _ := behavior.NewEngine([]behavior.Instance{cfg.Instance})

	var warnings bytes.Buffer

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		nil,
		nil,
		engine,
		driftRepo,
		&warnings,
	)

	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/foo",
		SetProps: map[string]any{"status": "active"},
	}); modifyErr != nil {
		test.Fatalf("Modify (recovery): %v", modifyErr)
	}

	if !strings.Contains(warnings.String(), "blocked") {
		test.Errorf("warnings = %q, want mention of 'blocked'", warnings.String())
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 || rows[0].ObservedStatus != "blocked" {
		test.Errorf("drift rows = %+v, want one row for 'blocked'", rows)
	}
}

func TestModify_HookCleanPassClearsDrift(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	driftRepo := index.NewWorkflowDriftRepo(store)

	// Seed: a drift row already present for the node we're about to modify.
	if appendErr := driftRepo.Append(index.WorkflowDriftRow{
		NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "stale", Property: "status", ObservedAt: 1,
	}); appendErr != nil {
		test.Fatalf("seed drift Append: %v", appendErr)
	}

	seed := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		nil, nil, nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"status": "pending"},
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	cfg := workflowConfigForTest(test)
	engine, _ := behavior.NewEngine([]behavior.Instance{cfg.Instance})

	service := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		nil, nil, engine, driftRepo, io.Discard,
	)

	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/foo",
		SetProps: map[string]any{"status": "active"}, // legal: pending → active
	}); modifyErr != nil {
		test.Fatalf("Modify (clean): %v", modifyErr)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 0 {
		test.Errorf("drift after clean Modify = %+v, want empty (clean pass clears)", rows)
	}
}

// workflowConfigForTest returns a workflow Instance with the standard
// 3-state machine. Helper used by the Modify recovery + clean tests.
type workflowConfigBundle struct {
	Instance behavior.Instance
}

func workflowConfigForTest(test *testing.T) workflowConfigBundle {
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
		test.Fatalf("decode: %v", decodeErr)
	}

	primitive := decoded.Behaviors["workflow"]["tickets"]

	instance, newErr := workflow.Kind{}.NewInstance("tickets", primitive, &meta)

	if newErr != nil {
		test.Fatalf("workflow.NewInstance: %v", newErr)
	}

	return workflowConfigBundle{Instance: instance}
}

func TestService_Modify_EnqueuesEmbed(test *testing.T) {
	root := test.TempDir()
	store := openTempIndex(test, root)
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	service := node.NewServiceWithEmbedQueue(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		queueRepo,
	)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/hi.md", Type: "note"}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	queueRepo.Drain(100) // clear from Create

	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:       "notes/hi",
		SetProps: map[string]any{"priority": 1},
	}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	depth, _ := queueRepo.Depth()

	if depth != 1 {
		test.Errorf("depth = %d, want 1", depth)
	}
}

func TestCreate_PropertyRequiredMissingRejects(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		decls,
		index.NewPropertyDriftRepo(store),
		nil, nil, io.Discard,
	)

	_, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
	})

	if createErr == nil || !strings.Contains(createErr.Error(), "summary") {
		test.Errorf("Create: expected required-missing error, got %v", createErr)
	}

	// File must NOT exist (hard error aborts before write).
	if _, statErr := os.Stat(filepath.Join(root, "tickets/foo.md")); !os.IsNotExist(statErr) {
		test.Errorf("file present after rejection; statErr = %v", statErr)
	}
}

func TestCreate_PropertyTypeMismatchRejects(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "priority", Type: "int"}}},
	}

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		decls,
		index.NewPropertyDriftRepo(store),
		nil, nil, io.Discard,
	)

	_, createErr := service.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"priority": "high"},
	})

	if createErr == nil || !strings.Contains(createErr.Error(), "priority") {
		test.Errorf("Create: expected type-mismatch error, got %v", createErr)
	}
}

func TestCreate_PropertyUndeclaredWritesAndDrifts(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
	}

	driftRepo := index.NewPropertyDriftRepo(store)

	var warnings bytes.Buffer

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		decls,
		driftRepo,
		nil, nil, &warnings,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"summary": "hi", "assignee": "bob"},
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if !strings.Contains(warnings.String(), "assignee") {
		test.Errorf("warnings = %q, want mention of assignee", warnings.String())
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 || rows[0].Property != "assignee" || rows[0].Kind != "undeclared-property" {
		test.Errorf("drift rows = %+v", rows)
	}
}
