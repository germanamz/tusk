package node

import (
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
)

// ParseContentFile parses a workspace file into a *Node, dispatching by file
// extension: .html/.htm files go through ParseHTMLFile (their id retains the
// extension), everything else through the markdown ParseFile (id strips the
// extension). This is the single content-kind dispatch shared by the reindex
// pipeline, the embed re-parse, and the node service read path, so every reader
// turns a file into a node the same way.
func ParseContentFile(relPath string, content []byte) (*Node, error) {
	return ParseContentFileAs(relPath, content, "")
}

// ParseContentFileAs is ParseContentFile with ParseFileAs's fallback type for
// markdown. HTML is never adopted: an untyped page is far more often a build
// artifact or a vendored doc than a note, so it still needs its tusk:type
// meta (#790).
func ParseContentFileAs(relPath string, content []byte, fallbackType string) (*Node, error) {
	if IsHTMLPath(relPath) {
		return ParseHTMLFile(relPath, content)
	}

	return ParseFileAs(relPath, content, fallbackType)
}

// IsHTMLPath reports whether a workspace path is an HTML content kind
// (.html or .htm).
func IsHTMLPath(relPath string) bool {
	switch filepath.Ext(relPath) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

// indexedType returns the type the index holds for the file at relPath, or ""
// when nodes is nil or the file has no node row. A file adopted under the
// workspace default type (#790) has no type of its own, so every re-parse
// after indexing passes this as the fallback; reindex alone decides, from the
// manifest, which untyped files are nodes.
func indexedType(nodes *index.NodeRepo, relPath string) string {
	if nodes == nil {
		return ""
	}

	row, getErr := nodes.Get(nodeIDForPath(relPath))

	if getErr != nil {
		return ""
	}

	return row.Type
}

// fileNodeType returns the indexed type of the file row belongs to: the row's
// own type for a file node, the file node's for one of its sub-units (whose
// own type is section, paragraph and so on). Writers and readers addressed at
// a sub-unit id still parse the whole file, so this is their fallback.
func fileNodeType(nodes *index.NodeRepo, row *index.NodeRow) string {
	if row.ID == nodeIDForPath(row.Path) {
		return row.Type
	}

	return indexedType(nodes, row.Path)
}

// nodeIDForPath derives a node id from a workspace-relative path, delegating to
// index.NodeIDForPath — the single id rule shared with the reindex walk and the
// parse dispatch. A retained-extension kind (HTML, MDX) keeps its extension
// (foo.html -> "foo.html", foo.mdx -> "foo.mdx") so it never collides with a
// same-stem markdown note, while markdown strips it (foo.md -> "foo"). Keeping
// this in lockstep with the parse dispatch is what lets a rename mint an id the
// reindex re-parse re-derives identically — computing it any other way (e.g.
// stripping the extension unconditionally) mints a phantom row whose id and
// path disagree.
func nodeIDForPath(relPath string) string {
	return index.NodeIDForPath(relPath)
}
