package node

import "path/filepath"

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
