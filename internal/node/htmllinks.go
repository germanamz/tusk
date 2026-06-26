package node

import (
	"net/url"
	"path"
	"strings"

	"github.com/germanamz/tusk/internal/manifest"
)

// ResolveHTMLLinks turns a node's raw <a href> values into target node ids,
// resolving each path-relative href against sourcePath's directory. HTML node
// ids retain their extension (foo.html -> id "foo.html"), so a sibling href
// like "topic-map.html" from "mml/index.html" resolves directly to the target
// id "mml/topic-map.html".
//
// Hrefs that cannot name a vault node are dropped: external URLs (any scheme,
// e.g. https:, mailto:), protocol-relative ("//host/…"), in-page anchors
// ("#…"), empty values, and links that escape the vault root ("../…" above
// root). Query strings and fragments are stripped. Results are unique in
// first-seen order, mirroring ExtractWikilinks.
func ResolveHTMLLinks(sourcePath string, hrefs []string) []string {
	dir := path.Dir(sourcePath)

	seen := map[string]struct{}{}
	var ordered []string

	for _, href := range hrefs {
		parsed, parseErr := url.Parse(strings.TrimSpace(href))

		if parseErr != nil {
			continue
		}

		// Drop external (scheme set) and protocol-relative ("//host") links —
		// neither names a local vault node.
		if parsed.IsAbs() || parsed.Host != "" {
			continue
		}

		linkPath := parsed.Path

		if linkPath == "" {
			// Pure anchor ("#section") or empty href.
			continue
		}

		var target string

		if strings.HasPrefix(linkPath, "/") {
			// Root-relative: resolve against the vault root, not the source dir.
			target = path.Clean(strings.TrimPrefix(linkPath, "/"))
		} else {
			target = path.Join(dir, linkPath)
		}

		// path.Join/Clean already collapse "." and "..". A leading ".." means
		// the link escaped the vault root and cannot name a node.
		if target == "." || target == ".." || strings.HasPrefix(target, "../") {
			continue
		}

		if _, already := seen[target]; already {
			continue
		}

		seen[target] = struct{}{}
		ordered = append(ordered, target)
	}

	return ordered
}

// MaterializeHTMLLinks appends a node's resolved <a href> targets to every edge
// type flagged with `wikilinks = true`, bridging HTML's native links into the
// same edge model markdown wikilinks use. It is the HTML counterpart of
// MaterializeWikilinks: a no-op for nodes without HTMLLinks (i.e. markdown).
// Idempotent per edge via appendUnique.
func MaterializeHTMLLinks(parsed *Node, edgeTypes manifest.EdgeTypes) {
	if len(parsed.HTMLLinks) == 0 {
		return
	}

	targets := ResolveHTMLLinks(parsed.Path, parsed.HTMLLinks)

	for name, edgeType := range edgeTypes {
		if !edgeType.Wikilinks {
			continue
		}

		for _, target := range targets {
			parsed.Edges[name] = appendUnique(parsed.Edges[name], target)
		}
	}
}
