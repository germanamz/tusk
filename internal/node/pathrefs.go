package node

import (
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/pathref"
)

// PathRefs returns the path refs parsed records, once for each edge type that
// sets paths = true and lists the page's type in from: the paths its body
// names (inline code, links and images in markdown; <code>, <a href> and
// <img src> in HTML), plus, under a paths-only type, the paths its frontmatter
// value names. Those come from parsed.PathValues, so ResolveEdges must have
// run. file is the whole file as read, so markdown lines count from its top
// under scheme. Nil when no paths edge type applies to the page.
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
		mentions = pathref.HTML(parsed.Path, parsed.HTMLCode, slices.Concat(parsed.HTMLLinks, parsed.HTMLImages))
	} else {
		mentions = pathref.Markdown(parsed.Path, parsed.Body, file, parsed.BodyOffset, scheme)
	}

	declaredLines := declaredPathLines(file, parsed.PathValues, scheme)
	rows := make([]index.PathRefRow, 0, len(types)*len(mentions))

	for _, edgeType := range types {
		for _, target := range parsed.PathValues[edgeType] {
			rows = append(rows, index.PathRefRow{
				SourceID: parsed.ID,
				Type:     edgeType,
				Target:   target,
				Line:     declaredLines[edgeType][target],
			})
		}

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

// declaredPathLines returns the file line each frontmatter path value in
// pathValues is written on, by edge type and then target, numbered under
// scheme. A value it can't place (HTML keeps its values in <meta> tags, and a
// YAML shape the locator doesn't follow) is absent, so its ref gets line 0.
func declaredPathLines(file []byte, pathValues map[string][]string, scheme linenum.Scheme) map[string]map[string]int {
	if len(pathValues) == 0 {
		return nil
	}

	start, end, hasFrontmatter := frontmatterSpan(file)

	if !hasFrontmatter {
		return nil
	}

	yamlText := file[start:end]

	var root yaml.Node

	if unmarshalErr := yaml.Unmarshal(yamlText, &root); unmarshalErr != nil {
		return nil
	}

	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil
	}

	table := linenum.NewTable(file, scheme)
	lines := map[string]map[string]int{}
	mapping := root.Content[0].Content

	for pairIdx := 0; pairIdx+1 < len(mapping); pairIdx += 2 {
		key := mapping[pairIdx].Value

		if _, declared := pathValues[key]; !declared {
			continue
		}

		for _, scalar := range collectEdgeScalars(mapping[pairIdx+1], false) {
			target, isPath := pathref.Declared(scalar.node.Value)

			if !isPath {
				continue
			}

			offset, located := byteOffsetAt(yamlText, scalar.node.Line, scalar.node.Column)

			if !located {
				continue
			}

			if lines[key] == nil {
				lines[key] = map[string]int{}
			}

			if _, seen := lines[key][target]; !seen {
				lines[key][target] = table.Line(start + offset)
			}
		}
	}

	return lines
}

// syncPathRefs re-derives a page's path refs from the bytes just written for it
// and replaces the stored set. The node service calls it wherever it rewrites a
// page's edges, because its writes stamp file_state and the next reindex skips
// the file. A parse failure, or a frontmatter edge value of the wrong shape,
// leaves the refs as they were: the write itself succeeded, and reindex reports
// the file.
func syncPathRefs(edges *index.EdgeRepo, relPath string, file []byte, edgeTypes manifest.EdgeTypes, scheme linenum.Scheme) error {
	if edges == nil {
		return nil
	}

	parsed, parseErr := ParseContentFile(relPath, file)

	if parseErr != nil {
		return nil
	}

	if resolveErr := ResolveEdges(parsed, edgeTypes); resolveErr != nil {
		return nil
	}

	return edges.ReplacePathRefs(parsed.ID, PathRefs(parsed, file, edgeTypes, scheme))
}
