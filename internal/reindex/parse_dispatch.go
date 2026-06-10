package reindex

import (
	"path/filepath"

	"github.com/germanamz/tusk/internal/node"
)

// parseContentFile parses a workspace file into a *node.Node, dispatching by
// extension: HTML kinds go through node.ParseHTMLFile (id retains the
// extension), everything else through the markdown node.ParseFile (id strips
// the extension). This is a thin internal switch on the content kind, not a
// registry — the only structural seam HTML indexing adds to the pipeline.
func parseContentFile(relPath string, content []byte) (*node.Node, error) {
	// keep in sync — import cycle (reindex imports embed) prevents sharing
	switch filepath.Ext(relPath) {
	case ".html", ".htm":
		return node.ParseHTMLFile(relPath, content)
	default:
		return node.ParseFile(relPath, content)
	}
}
