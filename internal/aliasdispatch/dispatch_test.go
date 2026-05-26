package aliasdispatch_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/status"
)

// setupWorkspace builds a tiny on-disk workspace: a workspace root with a
// single markdown file and an in-memory(ish) index populated with the
// matching NodeRow. Returns the deps suitable for the dispatcher.
func setupWorkspace(test *testing.T) aliasdispatch.Deps {
	test.Helper()

	root := test.TempDir()
	indexPath := filepath.Join(root, "index.db")

	store, openErr := index.Open(indexPath)

	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	// Drop a node on disk + in the index so node get + node list both work.
	relPath := "notes/hello.md"
	absPath := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	const fileBody = `---
type: note
title: Hello
---

Hello world.
`

	if writeErr := os.WriteFile(absPath, []byte(fileBody), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	nodes := index.NewNodeRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{
		ID:             "notes/hello",
		Type:           "note",
		Path:           relPath,
		Title:          "Hello",
		PropertiesJSON: "{}",
		LastChecksum:   "abc",
	}); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	edges := index.NewEdgeRepo(store)
	embedQueue := index.NewEmbedQueueRepo(store)
	embeddings := index.NewEmbeddingRepo(store)
	metaRepo := index.NewMetaRepo(store)

	loaded := &manifest.Manifest{
		Workspace: manifest.WorkspaceSection{Name: "test"},
		NodeTypes: map[string]manifest.NodeType{
			"note": {Properties: nil},
		},
	}

	return aliasdispatch.Deps{
		Database:      store.DB(),
		Manifest:      loaded,
		WorkspaceRoot: root,
		NodeService:   node.NewService(root, nodes),
		Nodes:         nodes,
		Edges:         edges,
		EmbedQueue:    embedQueue,
		Embeddings:    embeddings,
		Meta:          metaRepo,
	}
}

func validatedAlias(test *testing.T, command string, args map[string]any) manifest.Alias {
	test.Helper()

	loaded := &manifest.Manifest{
		Aliases: map[string]manifest.Alias{
			"test-alias": {
				Name:    "test-alias",
				Command: command,
				Args:    args,
			},
		},
	}

	// Use a permissive introspector so type validation doesn't reject the
	// args (the dispatcher's adapters re-coerce types anyway).
	manifest.ValidateAliases(loaded, func(_ string) ([]manifest.FlagSpec, bool) {
		return permissiveFlags(), true
	})

	alias, ok := loaded.Aliases["test-alias"]

	if !ok {
		test.Fatalf("alias not in validated set; errors=%v", loaded.AliasErrors)
	}

	return alias
}

func permissiveFlags() []manifest.FlagSpec {
	return []manifest.FlagSpec{
		{Name: "filter", Kind: "string"},
		{Name: "sort", Kind: "string"},
		{Name: "take", Kind: "int"},
		{Name: "skip", Kind: "int"},
		{Name: "include", Kind: "stringSlice"},
		{Name: "fields", Kind: "stringSlice"},
		{Name: "semantic", Kind: "string"},
		{Name: "min-score", Kind: "string"},
		{Name: "from", Kind: "string"},
		{Name: "to", Kind: "string"},
		{Name: "type", Kind: "string"},
		{Name: "no-migrate", Kind: "bool"},
	}
}

func TestDispatcher_NodeList(test *testing.T) {
	deps := setupWorkspace(test)
	dispatcher := aliasdispatch.NewDispatcher(deps)

	alias := validatedAlias(test, "node list", map[string]any{
		"filter": "type=note",
	})

	result, runErr := dispatcher.Run(context.Background(), alias)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Kind != aliasdispatch.KindNodeList {
		test.Errorf("Kind = %q, want %q", result.Kind, aliasdispatch.KindNodeList)
	}

	listResult, ok := result.Result.(*query.ListResult)

	if !ok {
		test.Fatalf("Result type = %T, want *query.ListResult", result.Result)
	}

	if len(listResult.Rows) != 1 {
		test.Fatalf("Rows len = %d, want 1: %+v", len(listResult.Rows), listResult.Rows)
	}

	if listResult.Rows[0].ID != "notes/hello" {
		test.Errorf("Rows[0].ID = %q, want notes/hello", listResult.Rows[0].ID)
	}
}

func TestDispatcher_NodeGet(test *testing.T) {
	deps := setupWorkspace(test)
	dispatcher := aliasdispatch.NewDispatcher(deps)

	alias := validatedAlias(test, "node get", map[string]any{
		"id": "notes/hello",
	})

	result, runErr := dispatcher.Run(context.Background(), alias)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Kind != aliasdispatch.KindNodeGet {
		test.Errorf("Kind = %q, want %q", result.Kind, aliasdispatch.KindNodeGet)
	}

	getResult, ok := result.Result.(*node.GetResult)

	if !ok {
		test.Fatalf("Result type = %T, want *node.GetResult", result.Result)
	}

	if getResult.Node.ID != "notes/hello" {
		test.Errorf("Node.ID = %q, want notes/hello", getResult.Node.ID)
	}
}

func TestDispatcher_Query(test *testing.T) {
	deps := setupWorkspace(test)
	dispatcher := aliasdispatch.NewDispatcher(deps)

	alias := validatedAlias(test, "query", map[string]any{
		"filter": "type=note",
	})

	result, runErr := dispatcher.Run(context.Background(), alias)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Kind != aliasdispatch.KindQuery {
		test.Errorf("Kind = %q, want %q", result.Kind, aliasdispatch.KindQuery)
	}

	queryResult, ok := result.Result.(*query.Result)

	if !ok {
		test.Fatalf("Result type = %T, want *query.Result", result.Result)
	}

	if len(queryResult.Rows) != 1 {
		test.Errorf("Rows len = %d, want 1", len(queryResult.Rows))
	}
}

func TestDispatcher_EdgeList(test *testing.T) {
	deps := setupWorkspace(test)
	dispatcher := aliasdispatch.NewDispatcher(deps)

	// Insert one edge so the list returns something.
	if upsertErr := deps.Edges.UpsertAll("notes/hello", "notes/hello.md", []index.EdgeRow{
		{Type: "references", SourceID: "notes/hello", TargetID: "notes/world", SourcePath: "notes/hello.md", Kind: "direct"},
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	alias := validatedAlias(test, "edge list", map[string]any{
		"from": "notes/hello",
	})

	result, runErr := dispatcher.Run(context.Background(), alias)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Kind != aliasdispatch.KindEdgeList {
		test.Errorf("Kind = %q, want %q", result.Kind, aliasdispatch.KindEdgeList)
	}

	edgeResult, ok := result.Result.(*index.EdgeListResult)

	if !ok {
		test.Fatalf("Result type = %T, want *index.EdgeListResult", result.Result)
	}

	if len(edgeResult.Rows) != 1 {
		test.Fatalf("Rows len = %d, want 1: %+v", len(edgeResult.Rows), edgeResult.Rows)
	}

	if edgeResult.Rows[0].TargetID != "notes/world" {
		test.Errorf("Rows[0].TargetID = %q, want notes/world", edgeResult.Rows[0].TargetID)
	}
}

func TestDispatcher_Doctor(test *testing.T) {
	deps := setupWorkspace(test)
	dispatcher := aliasdispatch.NewDispatcher(deps)

	alias := validatedAlias(test, "doctor", map[string]any{
		"no-migrate": true,
	})

	result, runErr := dispatcher.Run(context.Background(), alias)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Kind != aliasdispatch.KindDoctor {
		test.Errorf("Kind = %q, want %q", result.Kind, aliasdispatch.KindDoctor)
	}

	doctorResult, ok := result.Result.(*doctor.Result)

	if !ok {
		test.Fatalf("Result type = %T, want *doctor.Result", result.Result)
	}

	if doctorResult.Report == nil {
		test.Errorf("Report = nil; want non-nil")
	}
}

func TestDispatcher_Status(test *testing.T) {
	deps := setupWorkspace(test)
	dispatcher := aliasdispatch.NewDispatcher(deps)

	alias := validatedAlias(test, "status", nil)

	result, runErr := dispatcher.Run(context.Background(), alias)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Kind != aliasdispatch.KindStatus {
		test.Errorf("Kind = %q, want %q", result.Kind, aliasdispatch.KindStatus)
	}

	statusResult, ok := result.Result.(*status.Result)

	if !ok {
		test.Fatalf("Result type = %T, want *status.Result", result.Result)
	}

	if statusResult.NodesByType["note"] != 1 {
		test.Errorf("NodesByType[note] = %d, want 1", statusResult.NodesByType["note"])
	}
}

func TestDispatcher_ListAliases(test *testing.T) {
	deps := setupWorkspace(test)
	deps.Manifest.Aliases = map[string]manifest.Alias{
		"zeta":   {Name: "zeta", Command: "status"},
		"alpha":  {Name: "alpha", Command: "status"},
		"middle": {Name: "middle", Command: "status"},
	}

	dispatcher := aliasdispatch.NewDispatcher(deps)
	listed := dispatcher.ListAliases()

	if len(listed) != 3 {
		test.Fatalf("ListAliases len = %d, want 3", len(listed))
	}

	wantOrder := []string{"alpha", "middle", "zeta"}

	for index, want := range wantOrder {
		if listed[index].Name != want {
			test.Errorf("ListAliases[%d].Name = %q, want %q", index, listed[index].Name, want)
		}
	}
}

func TestDispatcher_UnknownVerb(test *testing.T) {
	deps := setupWorkspace(test)
	dispatcher := aliasdispatch.NewDispatcher(deps)

	// Cannot get a validated alias for an unknown verb — build by hand.
	alias := manifest.Alias{Name: "x", Command: "no-such-verb"}

	_, runErr := dispatcher.Run(context.Background(), alias)

	if runErr == nil {
		test.Fatalf("Run: expected error for unknown verb, got nil")
	}
}
