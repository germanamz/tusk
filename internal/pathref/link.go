package pathref

import (
	"net/url"
	"path"
	"strings"
)

// Link resolves a link destination written on the page at sourcePath (a
// workspace-relative path) into the workspace path it points at. A
// path-relative destination resolves against the page's directory, the way a
// browser or a markdown renderer reads it, and a root-relative one ("/docs/x.md")
// against the workspace root. Query strings and fragments are dropped and
// percent-escapes decoded.
//
// It returns false for a destination that names nothing in the workspace: an
// external URL (any scheme, e.g. https: or mailto:), a protocol-relative one
// ("//host/x"), an in-page anchor ("#intro"), an empty value, and one that
// climbs above the workspace root. Unlike Candidate it applies no shape test,
// since a link is an explicit reference: `[make](../Makefile)` counts.
func Link(sourcePath, destination string) (string, bool) {
	parsed, parseErr := url.Parse(strings.TrimSpace(destination))

	if parseErr != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path == "" {
		return "", false
	}

	var target string

	if rootRelative, isRooted := strings.CutPrefix(parsed.Path, "/"); isRooted {
		target = path.Clean(rootRelative)
	} else {
		target = path.Join(path.Dir(sourcePath), parsed.Path)
	}

	if target == "." || target == ".." || strings.HasPrefix(target, "../") || strings.HasPrefix(target, "/") {
		return "", false
	}

	return target, true
}
