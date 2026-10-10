package node

import (
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/pathref"
)

// PathRefs returns the path refs parsed records: the path candidates in its
// inline code (markdown code spans, or HTML <code> outside <pre>), once for each
// edge type that sets paths = true and lists the page's type in from. file is
// the whole file as read, so markdown lines count from its top under scheme.
// Nil when no paths edge type applies to the page.
func PathRefs(parsed *Node, file []byte, edgeTypes manifest.EdgeTypes, scheme linenum.Scheme) []index.PathRefRow {
	var types []string

	for _, name := range manifest.PathEdgeTypeNames(edgeTypes) {
		if edgeTypes[name].AllowsSource(parsed.Type) {
			types = append(types, name)
		}
	}

	if len(types) == 0 {
		return nil
	}

	var mentions []pathref.Mention

	if IsHTMLPath(parsed.Path) {
		mentions = pathref.Spans(parsed.HTMLCode)
	} else {
		mentions = pathref.Markdown(parsed.Body, file, parsed.BodyOffset, scheme)
	}

	rows := make([]index.PathRefRow, 0, len(types)*len(mentions))

	for _, edgeType := range types {
		for _, mention := range mentions {
			rows = append(rows, index.PathRefRow{
				SourceID: parsed.ID,
				Type:     edgeType,
				Target:   mention.Target,
				Line:     mention.Line,
			})
		}
	}

	return rows
}

// syncPathRefs re-derives a page's path refs from the bytes just written for it
// and replaces the stored set. The node service calls it wherever it rewrites a
// page's edges, because its writes stamp file_state and the next reindex skips
// the file. A parse failure leaves the refs as they were: the write itself
// succeeded, and reindex reports the unparseable file.
func syncPathRefs(edges *index.EdgeRepo, relPath string, file []byte, edgeTypes manifest.EdgeTypes, scheme linenum.Scheme) error {
	if edges == nil {
		return nil
	}

	parsed, parseErr := ParseContentFile(relPath, file)

	if parseErr != nil {
		return nil
	}

	return edges.ReplacePathRefs(parsed.ID, PathRefs(parsed, file, edgeTypes, scheme))
}
