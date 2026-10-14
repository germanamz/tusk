package reindex_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// nameLinkNodeTypes declares a task whose owner is a ref to a note, so a
// [[name]] ref value goes through the same lookup as a body link.
var nameLinkNodeTypes = map[string]manifest.NodeType{
	"note": {},
	"task": {Properties: []manifest.PropertyDecl{{Name: "owner", Type: "ref", To: "note"}}},
}

// nameLinkFixture writes a vault that links by file name (Obsidian's
// "shortest path when possible") and by relative path (#791).
func nameLinkFixture(test *testing.T) (string, *index.Index) {
	test.Helper()

	root := test.TempDir()

	writeRaw(test, root, "inbox/today.md", []byte("See [[auth-rfc]] and [[AUTH-RFC#S1|the intro]] and [[dup]].\n"))
	writeRaw(test, root, "inbox/rel.md", []byte("See [[../notes/auth-rfc]] and [[./today]].\n"))
	writeRaw(test, root, "notes/auth-rfc.md", []byte("# Intro\n\nBody.\n"))
	writeRaw(test, root, "a/dup.md", []byte("Dup.\n"))
	writeRaw(test, root, "b/dup.md", []byte("Dup.\n"))
	writeRaw(test, root, "tasks/t1.md", []byte("---\ntype: task\nowner: \"[[auth-rfc]]\"\n---\nDo it.\n"))

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return root, store
}

func runNameLinks(test *testing.T, root string, store *index.Index, linkCase string) {
	test.Helper()

	_, runErr := reindex.Run(withGen(store, reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		Edges:         index.NewEdgeRepo(store),
		EdgeTypes:     adoptEdgeTypes,
		NodeTypes:     nameLinkNodeTypes,
		PropertyDrift: index.NewPropertyDriftRepo(store),
		Manifest: &manifest.Manifest{
			Workspace: manifest.WorkspaceSection{DefaultType: "note", LinkCase: linkCase},
			EdgeTypes: adoptEdgeTypes,
			NodeTypes: nameLinkNodeTypes,
		},
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}
}

// edgeTargetsOf returns the sorted, distinct targets of every edge of
// edgeType declared by the file at sourcePath, its sub-units' included.
func edgeTargetsOf(test *testing.T, store *index.Index, sourcePath, edgeType string) []string {
	test.Helper()

	rows, listErr := index.NewEdgeRepo(store).ListBySourcePath(sourcePath)

	if listErr != nil {
		test.Fatalf("ListBySourcePath: %v", listErr)
	}

	var targets []string

	for _, row := range rows {
		if row.Type == edgeType && !slices.Contains(targets, row.TargetID) {
			targets = append(targets, row.TargetID)
		}
	}

	slices.Sort(targets)

	return targets
}

func wantEdgeTargets(test *testing.T, store *index.Index, sourcePath, edgeType string, want ...string) {
	test.Helper()

	if got := edgeTargetsOf(test, store, sourcePath, edgeType); !slices.Equal(got, want) {
		test.Errorf("%s %s targets = %q, want %q", sourcePath, edgeType, got, want)
	}
}

func TestRun_NameLinksResolveAndFollowTheVault(test *testing.T) {
	root, store := nameLinkFixture(test)

	runNameLinks(test, root, store, "")

	// Files drain in any order, yet every name resolves: one match is a
	// link, two (dup) leave the edge on the name for doctor to report.
	wantEdgeTargets(test, store, "inbox/today.md", "references", "dup", "notes/auth-rfc", "notes/auth-rfc#S1")
	wantEdgeTargets(test, store, "inbox/rel.md", "references", "inbox/today", "notes/auth-rfc")
	wantEdgeTargets(test, store, "tasks/t1.md", "owner", "notes/auth-rfc")

	// Matching exact case only, AUTH-RFC no longer finds auth-rfc.md.
	runNameLinks(test, root, store, "sensitive")

	wantEdgeTargets(test, store, "inbox/today.md", "references", "AUTH-RFC#S1", "dup", "notes/auth-rfc")

	// One duplicate deleted out of band: the name finds the other.
	if removeErr := os.Remove(filepath.Join(root, "b/dup.md")); removeErr != nil {
		test.Fatalf("remove: %v", removeErr)
	}

	runNameLinks(test, root, store, "")

	wantEdgeTargets(test, store, "inbox/today.md", "references", "a/dup", "notes/auth-rfc", "notes/auth-rfc#S1")

	// A second auth-rfc added out of band makes the name ambiguous, though
	// inbox/today.md itself never changed.
	writeRaw(test, root, "c/auth-rfc.md", []byte("Another.\n"))

	runNameLinks(test, root, store, "")

	wantEdgeTargets(test, store, "inbox/today.md", "references", "AUTH-RFC#S1", "a/dup", "auth-rfc")

	// The path-written link still names the note it always did.
	wantEdgeTargets(test, store, "inbox/rel.md", "references", "inbox/today", "notes/auth-rfc")
}
