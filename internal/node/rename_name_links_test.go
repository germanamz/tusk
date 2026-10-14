package node_test

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/wikilink"
)

// nameLinkVault is a workspace whose notes link each other by file name,
// relative path or workspace path (#791). Notes are written through
// Service.Create, which resolves name-form links like a reindex would.
type nameLinkVault struct {
	test     *testing.T
	root     string
	nodes    *index.NodeRepo
	edges    *index.EdgeRepo
	store    *index.Index
	service  *node.Service
	edgeType manifest.EdgeTypes
}

func newNameLinkVault(test *testing.T) *nameLinkVault {
	test.Helper()

	root := test.TempDir()
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	edgeTypes := wikilinkEdgeTypes()
	edgeTypes["relates-to"] = manifest.EdgeType{
		From: []string{"*"}, To: []string{"*"},
		Cardinality: manifest.CardinalityManyToMany,
	}

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	// A manifest turns on the sub-unit pass, so links are carried by the
	// sections and paragraphs around them as well as by the file.
	return &nameLinkVault{
		test:  test,
		root:  root,
		nodes: nodes,
		edges: edges,
		store: store,
		service: node.NewServiceWithDeps(node.ServiceDeps{
			WorkspaceRoot: root,
			Repo:          nodes,
			Edges:         edges,
			EdgeTypes:     edgeTypes,
			FileState:     index.NewFileStateRepo(store),
			WorkerID:      "test-worker",
			LeaseTTL:      time.Minute,
			Manifest:      &manifest.Manifest{EdgeTypes: edgeTypes},
		}),
		edgeType: edgeTypes,
	}
}

func (vault *nameLinkVault) create(relPath, body string, properties map[string]any) {
	vault.test.Helper()

	if _, createErr := vault.service.Create(node.CreateInput{
		RelPath: relPath, Type: "note", Body: []byte(body), Properties: properties,
	}); createErr != nil {
		vault.test.Fatalf("create %s: %v", relPath, createErr)
	}
}

func (vault *nameLinkVault) move(oldID, newPath string) *node.RenamePlan {
	vault.test.Helper()

	plan, renameErr := node.Rename(
		vault.root, vault.nodes, vault.edges,
		index.NewFileStateRepo(vault.store), "test-worker", time.Minute,
		vault.edgeType, nil, nil, linenum.DefaultScheme, wikilink.DefaultCase, oldID, newPath,
	)

	if renameErr != nil {
		vault.test.Fatalf("Rename %s → %s: %v", oldID, newPath, renameErr)
	}

	return plan
}

func (vault *nameLinkVault) read(relPath string) string {
	vault.test.Helper()

	content, readErr := os.ReadFile(filepath.Join(vault.root, relPath))

	if readErr != nil {
		vault.test.Fatalf("read %s: %v", relPath, readErr)
	}

	return string(content)
}

// targets lists the sorted, distinct targets of the edgeType edges declared
// by sourceID's file: its own and its sub-units'.
func (vault *nameLinkVault) targets(sourceID, edgeType string) []string {
	vault.test.Helper()

	rows, listErr := vault.edges.ListBySourcePath(index.FilePathForID(sourceID))

	if listErr != nil {
		vault.test.Fatalf("ListBySourcePath: %v", listErr)
	}

	var targets []string

	for _, row := range rows {
		if row.Type == edgeType && !slices.Contains(targets, row.TargetID) {
			targets = append(targets, row.TargetID)
		}
	}

	sort.Strings(targets)

	return targets
}

func (vault *nameLinkVault) wantTargets(sourceID, edgeType string, want ...string) {
	vault.test.Helper()

	if got := vault.targets(sourceID, edgeType); !slices.Equal(got, want) {
		vault.test.Errorf("%s %s targets = %v, want %v", sourceID, edgeType, got, want)
	}
}

func (vault *nameLinkVault) wantContains(relPath string, snippets ...string) {
	vault.test.Helper()

	content := vault.read(relPath)

	for _, snippet := range snippets {
		if !strings.Contains(content, snippet) {
			vault.test.Errorf("%s does not contain %q:\n%s", relPath, snippet, content)
		}
	}
}

func TestNameLinks_ResolveOnWrite(test *testing.T) {
	vault := newNameLinkVault(test)

	vault.create("inbox/today.md", "see [[Auth RFC]] and [[auth-rfc#S1|the intro]]\n", nil)
	vault.create("notes/auth-rfc.md", "# Intro\n\nBody.\n", map[string]any{"title": "Auth RFC"})

	// The link written before its target existed resolves once it does.
	vault.wantTargets("inbox/today", "references", "Auth RFC", "notes/auth-rfc#S1")

	vault.create("notes/Auth RFC.md", "Another.\n", nil)

	vault.wantTargets("inbox/today", "references", "notes/Auth RFC", "notes/auth-rfc#S1")
}

func TestRename_NameLinkSurvivesAFolderMove(test *testing.T) {
	vault := newNameLinkVault(test)

	vault.create("notes/auth-rfc.md", "# Intro\n\nBody.\n", nil)
	vault.create("inbox/today.md", "see [[auth-rfc]] and [[auth-rfc#S1|the intro]]\n", nil)

	vault.wantTargets("inbox/today", "references", "notes/auth-rfc", "notes/auth-rfc#S1")

	vault.move("notes/auth-rfc", "archive/auth-rfc.md")

	vault.wantContains("inbox/today.md", "see [[auth-rfc]] and [[auth-rfc#S1|the intro]]")
	vault.wantTargets("inbox/today", "references", "archive/auth-rfc", "archive/auth-rfc#S1")
}

func TestRename_NameLinkFollowsANewName(test *testing.T) {
	vault := newNameLinkVault(test)

	vault.create("notes/auth-rfc.md", "# Intro\n\nBody.\n", nil)
	vault.create("inbox/today.md", "see [[auth-rfc]] and [[ Auth-RFC#S1|the intro]]\n", map[string]any{"relates-to": "[[auth-rfc|RFC]]"})

	vault.move("notes/auth-rfc", "notes/rfc-auth.md")

	vault.wantContains("inbox/today.md",
		"see [[rfc-auth]] and [[rfc-auth#S1|the intro]]",
		`relates-to: "[[rfc-auth|RFC]]"`,
	)
	vault.wantTargets("inbox/today", "references", "notes/rfc-auth", "notes/rfc-auth#S1")
	vault.wantTargets("inbox/today", "relates-to", "notes/rfc-auth")
}

// A move that gives the note a name another note already has would leave
// both notes' [[name]] links ambiguous. Both sides fall back to full ids, so
// every link still names the note it named.
func TestRename_NameCollisionRewritesBothSidesToFullIDs(test *testing.T) {
	vault := newNameLinkVault(test)

	vault.create("notes/auth-rfc.md", "Body.\n", nil)
	vault.create("other/spec.md", "Body.\n", nil)
	vault.create("inbox/a.md", "see [[auth-rfc]]\n", nil)
	vault.create("inbox/b.md", "see [[spec]]\n", nil)

	plan := vault.move("notes/auth-rfc", "notes/spec.md")

	vault.wantContains("inbox/a.md", "see [[notes/spec]]")
	vault.wantContains("inbox/b.md", "see [[other/spec]]")
	vault.wantTargets("inbox/a", "references", "notes/spec")
	vault.wantTargets("inbox/b", "references", "other/spec")

	if !slices.Contains(plan.AffectedFiles, "inbox/b.md") {
		test.Errorf("AffectedFiles = %v, want it to list inbox/b.md", plan.AffectedFiles)
	}
}

// A note at the workspace root is linked by its id, which is also its name.
// Moved into a folder, the name still finds it, so the link keeps its form.
func TestRename_RootNoteKeepsItsShortLink(test *testing.T) {
	vault := newNameLinkVault(test)

	vault.create("inbox.md", "Body.\n", nil)
	vault.create("notes/today.md", "see [[inbox]]\n", nil)

	vault.move("inbox", "areas/inbox.md")

	vault.wantContains("notes/today.md", "see [[inbox]]")
	vault.wantTargets("notes/today", "references", "areas/inbox")
}

func TestRename_RelativeLinksFollowBothEnds(test *testing.T) {
	vault := newNameLinkVault(test)

	vault.create("inbox/today.md", "Body.\n", nil)
	vault.create("notes/target.md", "back to [[../inbox/today]]\n", nil)
	vault.create("inbox/referrer.md", "see [[../notes/target]]\n", nil)

	vault.wantTargets("inbox/referrer", "references", "notes/target")
	vault.wantTargets("notes/target", "references", "inbox/today")

	vault.move("notes/target", "archive/deep/target.md")

	// A link to the moved note is re-rendered from the referrer's folder.
	vault.wantContains("inbox/referrer.md", "see [[../archive/deep/target]]")
	vault.wantTargets("inbox/referrer", "references", "archive/deep/target")

	// The moved note's own relative link is re-rendered from its new folder,
	// so it still names the same note.
	vault.wantContains("archive/deep/target.md", "back to [[../../inbox/today]]")
	vault.wantTargets("archive/deep/target", "references", "inbox/today")
}

func TestDelete_NameLinkFallsToTheRemainingFile(test *testing.T) {
	vault := newNameLinkVault(test)

	vault.create("a/dup.md", "Body.\n", nil)
	vault.create("b/dup.md", "Body.\n", nil)
	vault.create("inbox/today.md", "see [[dup]]\n", nil)

	vault.wantTargets("inbox/today", "references", "dup")

	if deleteErr := node.Delete(
		vault.root, vault.nodes, vault.edges, index.NewFileStateRepo(vault.store), nil,
		"test-worker", time.Minute, wikilink.DefaultCase, "b/dup",
	); deleteErr != nil {
		test.Fatalf("Delete: %v", deleteErr)
	}

	vault.wantTargets("inbox/today", "references", "a/dup")
}

func TestRemoveEdge_MatchesAWikilinkValue(test *testing.T) {
	vault := newNameLinkVault(test)

	vault.create("notes/auth-rfc.md", "Body.\n", nil)
	vault.create("inbox/today.md", "Body.\n", map[string]any{"relates-to": []any{"[[auth-rfc]]", "[[../notes/auth-rfc|again]]"}})

	vault.wantTargets("inbox/today", "relates-to", "notes/auth-rfc")

	if removeErr := vault.service.RemoveEdge("relates-to", "inbox/today", "notes/auth-rfc"); removeErr != nil {
		test.Fatalf("RemoveEdge: %v", removeErr)
	}

	vault.wantTargets("inbox/today", "relates-to")

	if content := vault.read("inbox/today.md"); strings.Contains(content, "relates-to") {
		test.Errorf("relates-to left in frontmatter:\n%s", content)
	}
}
