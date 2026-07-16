package bookview

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// writeNodeFile writes body to root/relPath, creating parent directories.
func writeNodeFile(test *testing.T, root, relPath, body string) {
	test.Helper()

	full := filepath.Join(root, relPath)

	if mkdirErr := os.MkdirAll(filepath.Dir(full), 0o755); mkdirErr != nil {
		test.Fatalf("mkdir for %s: %v", relPath, mkdirErr)
	}

	if writeErr := os.WriteFile(full, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}

// getNode drives handleNode for nodeID and returns the recorder. The route
// registers {id...}, so the id arrives via PathValue rather than the URL alone.
func getNode(srv *Server, nodeID string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/node/"+nodeID, nil)
	req.SetPathValue("id", nodeID)

	srv.handleNode(rec, req)

	return rec
}

// TestNodeReadPayload pins the reading payload: metadata from the index row,
// the raw markdown body read fresh from disk with only frontmatter stripped
// (the frontend renders it, so markup must survive verbatim), and the file-level
// links split by direction.
func TestNodeReadPayload(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\n# Heading\n\nBody $x^2$ with *emphasis*.\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md", PropertiesJSON: `{"title":"A"}`},
		{ID: "b", Type: "note", Title: "B", Path: "b.md"},
		{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
	}}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "a", TargetID: "b", SourcePath: "a.md", Kind: "derived"},
		{Type: "depends-on", SourceID: "c", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: edges})

	rec := getNode(srv, "a")

	if rec.Code != http.StatusOK {
		test.Fatalf("code=%d want 200: %s", rec.Code, rec.Body.String())
	}

	var got NodeReadPayload

	if unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &got); unmarshalErr != nil {
		test.Fatalf("unmarshal %q: %v", rec.Body.Bytes(), unmarshalErr)
	}

	if got.ID != "a" || got.Type != "note" || got.Title != "A" || got.Path != "a.md" {
		test.Fatalf("metadata = %+v", got)
	}

	if string(got.Properties) != `{"title":"A"}` {
		test.Fatalf("properties=%s want %s", got.Properties, `{"title":"A"}`)
	}

	if strings.Contains(got.Markdown, "title: A") {
		test.Fatalf("frontmatter not stripped: %q", got.Markdown)
	}

	// The markup itself must survive: the browser renders it, so a stripped
	// heading hash or emphasis marker would reach the reader as plain prose.
	for _, want := range []string{"# Heading", "$x^2$", "*emphasis*"} {
		if !strings.Contains(got.Markdown, want) {
			test.Fatalf("markdown %q missing %q", got.Markdown, want)
		}
	}

	if len(got.Links.Out) != 1 || got.Links.Out[0] != (LinkRef{ID: "b", Title: "B", Type: "note", EdgeType: "references"}) {
		test.Fatalf("links.out=%+v", got.Links.Out)
	}

	if len(got.Links.In) != 1 || got.Links.In[0] != (LinkRef{ID: "c", Title: "C", Type: "spec", EdgeType: "depends-on"}) {
		test.Fatalf("links.in=%+v", got.Links.In)
	}
}

// wikilinksFor drives handleNode for nodeID and returns the resolved wikilink
// map. The map is the projection under test here; node_test.go's other cases
// pin the surrounding payload.
func wikilinksFor(test *testing.T, srv *Server, nodeID string) map[string]WikilinkTarget {
	test.Helper()

	rec := getNode(srv, nodeID)

	if rec.Code != http.StatusOK {
		test.Fatalf("code=%d want 200: %s", rec.Code, rec.Body.String())
	}

	var got NodeReadPayload

	if unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &got); unmarshalErr != nil {
		test.Fatalf("unmarshal %q: %v", rec.Body.Bytes(), unmarshalErr)
	}

	return got.Wikilinks
}

// TestNodeWikilinks pins the core of the reading UI's link rewriting: every
// [[target]] in the body arrives pre-resolved, so the frontend rewrites each
// one into an in-app link or a dead-link marker without a round trip per link.
// A target naming a real id resolves to it; one naming nothing resolves to
// Exists false rather than being absent from the map, so the client can tell
// "unresolved" from "not a link I asked about".
func TestNodeWikilinks(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nsee [[b]] and [[Ghost]]\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "b", Type: "note", Title: "B", Path: "b.md"},
	}}

	got := wikilinksFor(test, New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}}), "a")

	if got["b"] != (WikilinkTarget{ID: "b", Title: "B", Exists: true}) {
		test.Fatalf("wikilinks[b]=%+v want the id resolved with its title", got["b"])
	}

	ghost, present := got["Ghost"]

	if !present || ghost.Exists || ghost.ID != "" {
		test.Fatalf("wikilinks[Ghost]=%+v present=%v want a present, unresolved entry", ghost, present)
	}
}

// TestNodeWikilinksResolveByTitle pins the other form a wikilink takes: a
// target that names no id at all, but matches a node's title ([[Some Note]]
// rather than [[some-note]]). Without the title fallback every human-written
// link would render dead.
func TestNodeWikilinksResolveByTitle(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nsee [[Bee Note]]\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "notes/bee", Type: "note", Title: "Bee Note", Path: "notes/bee.md"},
	}}

	got := wikilinksFor(test, New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}}), "a")

	// Keyed on the raw target the client cuts out of the body, resolved to the
	// id it must navigate to — the two differ on this path, which is the point.
	if got["Bee Note"] != (WikilinkTarget{ID: "notes/bee", Title: "Bee Note", Exists: true}) {
		test.Fatalf("wikilinks[Bee Note]=%+v want the title resolved to its id", got["Bee Note"])
	}
}

// TestNodeWikilinksKeyOnAliasTarget pins the alias form: [[b|Bee]] links to b
// and displays "Bee". The label is presentation and is never resolved, so the
// map must key on "b" — the target segment, which is what the client keys on
// when it rewrites. Keying on the whole inner text would miss every aliased
// link and render it dead.
func TestNodeWikilinksKeyOnAliasTarget(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nsee [[b|Bee]]\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "b", Type: "note", Title: "B", Path: "b.md"},
	}}

	got := wikilinksFor(test, New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}}), "a")

	if got["b"] != (WikilinkTarget{ID: "b", Title: "B", Exists: true}) {
		test.Fatalf("wikilinks[b]=%+v want the alias stripped from the key", got["b"])
	}

	if _, present := got["b|Bee"]; present {
		test.Fatalf("wikilinks=%+v keyed on the alias suffix", got)
	}
}

// TestNodeWikilinksRollUpFragmentTarget pins the fragment ruling: [[c#S1]]
// resolves to the FILE c, not to the sub-unit row c#S1, matching the rails'
// rollup of sub-unit link ends. A sub-unit row carries its file's Path, so
// /api/node/c#S1 serves c's whole body under the section's title — a payload
// whose metadata contradicts its content. Rolling up loses nothing: the map is
// keyed on the raw target, so the client still holds "#S1" to anchor on.
func TestNodeWikilinksRollUpFragmentTarget(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nsee [[c#S1]] and [[c#S1|Section One]]\n")

	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		},
		sub: []index.NodeRow{subUnit("c#S1", "c", "spec", "Section 1", "c.md")},
	}

	got := wikilinksFor(test, New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}}), "a")

	// The sub-unit row exists and Get would resolve it — resolving to it is the
	// live failure this pins, not a hypothetical.
	if got["c#S1"] != (WikilinkTarget{ID: "c", Title: "C", Exists: true}) {
		test.Fatalf("wikilinks[c#S1]=%+v want the fragment rolled up to file c", got["c#S1"])
	}

	// Both spellings are the same target, so they collapse to the one key.
	if len(got) != 1 {
		test.Fatalf("wikilinks=%+v want one entry", got)
	}
}

// TestNodeWikilinksFragmentResolvesWithoutSubUnitRow pins why the rollup keys
// resolution on the file rather than the sub-unit row: a workspace that does
// not index sub-units has no c#S1 row to find, and a workspace that does can be
// mid-reindex. [[c#S1]] is still a live link into a note that exists, so
// verifying the sub-unit row would render every fragment link dead in the
// former and flicker in the latter.
func TestNodeWikilinksFragmentResolvesWithoutSubUnitRow(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nsee [[c#S1]]\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
	}}

	got := wikilinksFor(test, New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}}), "a")

	if got["c#S1"] != (WikilinkTarget{ID: "c", Title: "C", Exists: true}) {
		test.Fatalf("wikilinks[c#S1]=%+v want file c, resolved without a sub-unit row", got["c#S1"])
	}
}

// TestNodeWikilinksFragmentOfMissingFileUnresolved is the counterweight to the
// two rollup cases: rolling up must not resolve a fragment whose FILE is gone.
// Without this the rollup could pass by returning Exists true for anything with
// a "#" in it.
func TestNodeWikilinksFragmentOfMissingFileUnresolved(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nsee [[gone#S1]]\n")

	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	got := wikilinksFor(test, New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}}), "a")

	if got["gone#S1"].Exists {
		test.Fatalf("wikilinks[gone#S1]=%+v want unresolved", got["gone#S1"])
	}
}

// TestNodeWikilinksScopedToRenderedBody pins the extraction's scope: the map
// describes the Markdown field the client rewrites, and frontmatter is stripped
// out of that field. A frontmatter ref is a link the reader never sees as text,
// and it already reaches them through the links rails, so an entry for it could
// only be an unrewritable key.
func TestNodeWikilinksScopedToRenderedBody(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\nassignee: \"[[b]]\"\n---\nNo body links.\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "b", Type: "note", Title: "B", Path: "b.md"},
	}}

	got := wikilinksFor(test, New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}}), "a")

	if len(got) != 0 {
		test.Fatalf("wikilinks=%+v want none: the frontmatter ref is not in the rendered body", got)
	}
}

// TestNodeWikilinksMarshalEmptyObject guards the nil-map trap at the wire-byte
// level: a body with no links must marshal to {} rather than null. This is the
// live path, not a hypothetical — ExtractWikilinks returns a nil slice when a
// body has no links, and a nil map marshals to null, which would force every
// consumer to null-check before indexing. A JSON decode cannot tell {} from
// null, so only the raw bytes can pin it.
func TestNodeWikilinksMarshalEmptyObject(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nNo links here.\n")

	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}})

	body := getNode(srv, "a").Body.String()

	if !strings.Contains(body, `"wikilinks":{}`) {
		test.Fatalf("body=%s want wikilinks as {}", body)
	}
}

// TestNodeRouteServesSlashedID drives the real mux over HTTP rather than
// calling the handler directly. Node ids are path-derived, so a nested file's id
// contains slashes — the {id...} wildcard must capture the rest of the path and
// hand it over intact, which a direct SetPathValue call cannot prove.
func TestNodeRouteServesSlashedID(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "specs/deep/b.md", "---\ntitle: B\n---\n# Deep\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "specs/deep/b", Type: "spec", Title: "B", Path: "specs/deep/b.md"},
	}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/node/specs/deep/b")

	if getErr != nil {
		test.Fatalf("get: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	var got NodeReadPayload

	if decodeErr := json.NewDecoder(resp.Body).Decode(&got); decodeErr != nil {
		test.Fatalf("decode: %v", decodeErr)
	}

	if got.ID != "specs/deep/b" || got.Path != "specs/deep/b.md" {
		test.Fatalf("payload id=%q path=%q, want the slashed id intact", got.ID, got.Path)
	}

	if !strings.Contains(got.Markdown, "# Deep") {
		test.Fatalf("markdown=%q", got.Markdown)
	}
}

// TestNodeNotFound pins the 404: an id with no index row is a missing document,
// not an empty one. The links traversal walks edges and never verifies the focus
// node exists, so it would happily return zero links for an unknown id — without
// the Get check a typo'd id would render as a valid, blank page.
func TestNodeNotFound(test *testing.T) {
	srv := New(Deps{Root: test.TempDir(), Nodes: fakeNodes{}, Edges: fakeEdges{}})

	if rec := getNode(srv, "missing"); rec.Code != http.StatusNotFound {
		test.Fatalf("code=%d want 404", rec.Code)
	}
}

// TestNodeBodyMissingFromDisk pins the stale-index path: the row survives in the
// index but its file is gone. That is an unreadable source, not a missing node —
// 404 would tell the frontend to forget an id the index still lists.
func TestNodeBodyMissingFromDisk(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	srv := New(Deps{Root: test.TempDir(), Nodes: nodes, Edges: fakeEdges{}})

	if rec := getNode(srv, "a"); rec.Code != http.StatusServiceUnavailable {
		test.Fatalf("code=%d want 503", rec.Code)
	}
}

// TestNodeLinksMarshalEmptyArray guards the nil-slice trap at the wire-byte
// level: an isolated node's links must marshal to [] rather than null. A JSON
// decode cannot tell the two apart, so only the raw bytes can pin this.
func TestNodeLinksMarshalEmptyArray(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nBody\n")

	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}})

	body := getNode(srv, "a").Body.String()

	if !strings.Contains(body, `"links":{"out":[],"in":[]}`) {
		test.Fatalf("body=%s want links.out/links.in as []", body)
	}
}

// TestNodeEmptyPropertiesMarshalObject pins the empty-column path: NodeRow's
// PropertiesJSON is a plain string, so an unset column would emit a bare
// "properties": on the wire — syntactically invalid JSON that breaks the whole
// payload, not just the field.
func TestNodeEmptyPropertiesMarshalObject(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "Body only\n")

	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}})

	rec := getNode(srv, "a")

	if !strings.Contains(rec.Body.String(), `"properties":{}`) {
		test.Fatalf("body=%s want properties as {}", rec.Body.String())
	}

	var got NodeReadPayload

	if unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &got); unmarshalErr != nil {
		test.Fatalf("unmarshal %q: %v", rec.Body.Bytes(), unmarshalErr)
	}

	// A body with no frontmatter block is returned unchanged.
	if got.Markdown != "Body only\n" {
		test.Fatalf("markdown=%q want %q", got.Markdown, "Body only\n")
	}
}
