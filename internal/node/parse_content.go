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
	if IsHTMLPath(relPath) {
		return ParseHTMLFile(relPath, content)
	}

	return ParseFile(relPath, content)
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
