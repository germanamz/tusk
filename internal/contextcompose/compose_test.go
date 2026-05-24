package contextcompose_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/contextcompose"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// setupComposeWorkspace builds a tiny on-disk workspace with two indexed
// notes so pinned + recent + alias paths all have rows to return.
func setupComposeWorkspace(test *testing.T) (contextcompose.Deps, *manifest.Manifest) {
	test.Helper()

	root := test.TempDir()
	indexPath := filepath.Join(root, "index.db")

	store, openErr := index.Open(indexPath)

	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	writeNode(test, root, "notes/alpha.md", `---
type: note
title: Alpha
---

Alpha body.
`)

	writeNode(test, root, "notes/beta.md", `---
type: note
title: Beta
---

Beta body.
`)

	nodes := index.NewNodeRepo(store)

	for _, row := range []index.NodeRow{
		{ID: "notes/alpha", Type: "note", Path: "notes/alpha.md", Title: "Alpha", PropertiesJSON: "{}", LastChecksum: "a"},
		{ID: "notes/beta", Type: "note", Path: "notes/beta.md", Title: "Beta", PropertiesJSON: "{}", LastChecksum: "b"},
	} {
		if upsertErr := nodes.Upsert(row); upsertErr != nil {
			test.Fatalf("Upsert: %v", upsertErr)
		}
	}

	edges := index.NewEdgeRepo(store)
	embedQueue := index.NewEmbedQueueRepo(store)
	embeddings := index.NewEmbeddingRepo(store)
	metaRepo := index.NewMetaRepo(store)

	loaded := &manifest.Manifest{
		Workspace: manifest.WorkspaceSection{Name: "test"},
		NodeTypes: map[string]manifest.NodeType{"note": {}},
	}

	aliasDeps := aliasdispatch.Deps{
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

	dispatcher := aliasdispatch.NewDispatcher(aliasDeps)

	composeDeps := contextcompose.Deps{
		Manifest:      loaded,
		Dispatcher:    dispatcher,
		WorkspaceRoot: root,
		Database:      store.DB(),
	}

	return composeDeps, loaded
}

func writeNode(test *testing.T, root, relPath, body string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(abs, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}

func TestCompose_PinnedOnly(test *testing.T) {
	deps, loaded := setupComposeWorkspace(test)

	loaded.Context = &manifest.Context{
		Pinned: []string{"notes/alpha", "notes/beta"},
	}

	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if got, want := len(result.Pinned), 2; got != want {
		test.Fatalf("Pinned len = %d, want %d (%+v)", got, want, result.Pinned)
	}

	if result.Pinned[0].ID != "notes/alpha" {
		test.Errorf("Pinned[0].ID = %q, want notes/alpha", result.Pinned[0].ID)
	}

	if result.Pinned[0].Body == "" {
		test.Errorf("Pinned[0].Body empty; default include should expand body")
	}

	if len(result.MissingPinned) != 0 {
		test.Errorf("MissingPinned = %v, want empty", result.MissingPinned)
	}
}

func TestCompose_PinnedMissingIDsSurfaced(test *testing.T) {
	deps, loaded := setupComposeWorkspace(test)

	loaded.Context = &manifest.Context{
		Pinned: []string{"notes/alpha", "notes/ghost"},
	}

	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if got, want := len(result.Pinned), 1; got != want {
		test.Fatalf("Pinned len = %d, want %d", got, want)
	}

	if got, want := len(result.MissingPinned), 1; got != want {
		test.Fatalf("MissingPinned len = %d, want %d", got, want)
	}

	if result.MissingPinned[0] != "notes/ghost" {
		test.Errorf("MissingPinned[0] = %q", result.MissingPinned[0])
	}
}

func TestCompose_RecentOnly_NodeListAlias(test *testing.T) {
	deps, loaded := setupComposeWorkspace(test)

	loaded.Aliases = map[string]manifest.Alias{
		"recent-notes": {
			Name:    "recent-notes",
			Command: "node list",
			Args:    map[string]any{"filter": "type=note"},
		},
	}

	recent := loaded.Aliases["recent-notes"]

	loaded.Context = &manifest.Context{
		Recent: &recent,
	}

	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if got, want := len(result.Recent), 2; got != want {
		test.Fatalf("Recent len = %d, want %d", got, want)
	}
}

func TestCompose_AliasesOnly_RunsInParallel(test *testing.T) {
	deps, loaded := setupComposeWorkspace(test)

	loaded.Aliases = map[string]manifest.Alias{
		"all-notes": {
			Name:    "all-notes",
			Command: "node list",
			Args:    map[string]any{"filter": "type=note"},
		},
		"health": {
			Name:    "health",
			Command: "status",
		},
	}

	loaded.Context = &manifest.Context{
		Include: []string{"all-notes", "health"},
	}

	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if got, want := len(result.Aliases), 2; got != want {
		test.Fatalf("Aliases len = %d, want %d (%+v)", got, want, result.Aliases)
	}

	if result.Aliases["all-notes"] == nil {
		test.Errorf("Aliases[all-notes] missing")
	}

	if result.Aliases["health"] == nil {
		test.Errorf("Aliases[health] missing")
	}
}

func TestCompose_AllThree(test *testing.T) {
	deps, loaded := setupComposeWorkspace(test)

	loaded.Aliases = map[string]manifest.Alias{
		"all-notes": {
			Name:    "all-notes",
			Command: "node list",
			Args:    map[string]any{"filter": "type=note"},
		},
		"recent": {
			Name:    "recent",
			Command: "node list",
			Args:    map[string]any{"filter": "type=note"},
		},
	}

	recent := loaded.Aliases["recent"]

	loaded.Context = &manifest.Context{
		Pinned:  []string{"notes/alpha"},
		Recent:  &recent,
		Include: []string{"all-notes"},
	}

	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if len(result.Pinned) != 1 || result.Pinned[0].ID != "notes/alpha" {
		test.Errorf("Pinned = %+v", result.Pinned)
	}

	if len(result.Recent) != 2 {
		test.Errorf("Recent len = %d, want 2", len(result.Recent))
	}

	if len(result.Aliases) != 1 || result.Aliases["all-notes"] == nil {
		test.Errorf("Aliases = %+v", result.Aliases)
	}
}

func TestCompose_NoContextBlock_ReturnsEmpty(test *testing.T) {
	deps, _ := setupComposeWorkspace(test)

	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if len(result.Pinned) != 0 || len(result.Recent) != 0 || len(result.Aliases) != 0 {
		test.Errorf("expected empty result; got %+v", result)
	}
}

func TestCompose_IncludeOverridesPinnedExpansion(test *testing.T) {
	deps, loaded := setupComposeWorkspace(test)

	loaded.Context = &manifest.Context{
		Pinned: []string{"notes/alpha"},
	}

	// Request only edges; body should be omitted from the pinned row.
	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{
		Include: []string{"edges"},
	})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if len(result.Pinned) != 1 {
		test.Fatalf("Pinned len = %d, want 1", len(result.Pinned))
	}

	if result.Pinned[0].Body != "" {
		test.Errorf("Pinned[0].Body = %q, want empty when only edges requested", result.Pinned[0].Body)
	}
}

// TestCompose_RecentDefaultIncludeInjected confirms that an alias declared
// WITHOUT args.include picks up the digest-level default ([body, edges] when
// Request.Include is empty) so the recent rows arrive with body populated.
// Documents the contract called out in Request.Include's doc comment.
func TestCompose_RecentDefaultIncludeInjected(test *testing.T) {
	deps, loaded := setupComposeWorkspace(test)

	loaded.Aliases = map[string]manifest.Alias{
		"recent-notes": {
			Name:    "recent-notes",
			Command: "node list",
			Args:    map[string]any{"filter": "type=note"},
		},
	}

	recent := loaded.Aliases["recent-notes"]

	loaded.Context = &manifest.Context{
		Recent: &recent,
	}

	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if len(result.Recent) == 0 {
		test.Fatalf("Recent empty; want default-include injection to produce rows")
	}

	var bodiesPopulated int

	for _, row := range result.Recent {
		if row.Body != "" {
			bodiesPopulated++
		}
	}

	if bodiesPopulated == 0 {
		test.Errorf("expected recent rows to carry body via injected default include; got %+v", result.Recent)
	}
}

// TestCompose_RecentAliasIncludeWinsOverDefault confirms that an alias
// declaring its own args.include is NOT overwritten by the digest-level
// default. The author's explicit choice (here: edges only) survives.
func TestCompose_RecentAliasIncludeWinsOverDefault(test *testing.T) {
	deps, loaded := setupComposeWorkspace(test)

	loaded.Aliases = map[string]manifest.Alias{
		"recent-edges-only": {
			Name:    "recent-edges-only",
			Command: "node list",
			Args: map[string]any{
				"filter":  "type=note",
				"include": []any{"edges"},
			},
		},
	}

	recent := loaded.Aliases["recent-edges-only"]

	loaded.Context = &manifest.Context{
		Recent: &recent,
	}

	result, err := contextcompose.Compose(context.Background(), deps, contextcompose.Request{})

	if err != nil {
		test.Fatalf("Compose: %v", err)
	}

	if len(result.Recent) == 0 {
		test.Fatalf("Recent empty; want alias-declared include to produce rows")
	}

	for _, row := range result.Recent {
		if row.Body != "" {
			test.Errorf("row %q has body %q; alias declared include=[edges] only, default must not override", row.ID, row.Body)
		}
	}
}
