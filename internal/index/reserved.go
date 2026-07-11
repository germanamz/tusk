package index

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SubUnitIDSeparator joins a file id and a sub-unit's structural address into a
// sub-unit node id ("<fileID>#<address>"). It mirrors the row-id format the
// internal/subunit pipeline emits; it lives here so the indexing boundary can
// reject any file whose own id would contain it (such a file node is
// indistinguishable from a sub-unit of another file and gets swept away by that
// file's sub-unit sync — #683).
const SubUnitIDSeparator = "#"

// indexableExts is the single source of truth for which file extensions tusk
// treats as content nodes. It lives here — at the indexing boundary, beside
// ReservedIDReason — so both the reindex walk and the node write surface
// (Create / Rename) gate on the same set: a file the walk would never pick up
// must not be authored as a node, or its index row becomes a permanent phantom
// (#686).
var indexableExts = map[string]bool{
	".md":   true,
	".html": true,
	".htm":  true,
}

// IsIndexableExt reports whether relPath carries an extension tusk indexes as a
// content node (.md, .html, .htm).
func IsIndexableExt(relPath string) bool {
	return indexableExts[filepath.Ext(relPath)]
}

// ReservedIDReason reports why a workspace-relative markdown/HTML path cannot be
// indexed as a node because its derived id would collide with tusk's reserved
// id syntax, or "" when the path is safe to index. The node id is the path with
// its extension trimmed (see nodes.id).
//
// Two collisions make a path unindexable:
//
//   - A '#' anywhere in the id collides with the sub-unit id separator
//     "<fileID>#<address>". The file node would be indistinguishable from a
//     sub-unit of another file, so that file's sub-unit sync deletes it every
//     pass — silent data loss (#683 finding 3).
//   - A path starting with the reindex-queue prefix "reindex:" collides with the
//     embed_queue's reserved key namespace, so EnqueueReindex rejects it and the
//     whole walk aborts (#683 finding 4).
//
// Bracket names ("[wip] foo.md"), colons, spaces, and underscores are all safe
// and index normally.
func ReservedIDReason(relPath string) string {
	id := strings.TrimSuffix(relPath, filepath.Ext(relPath))

	if strings.Contains(id, SubUnitIDSeparator) {
		return fmt.Sprintf("id %q contains %q, the reserved sub-unit separator", id, SubUnitIDSeparator)
	}

	if strings.HasPrefix(relPath, ReindexNodeIDPrefix) {
		return fmt.Sprintf("path %q starts with %q, the reserved reindex-queue prefix", relPath, ReindexNodeIDPrefix)
	}

	return ""
}
