package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/pathref"
	"github.com/germanamz/tusk/internal/wikilink"
)

// llms-full.txt is the llmstxt.org companion to llms.txt: the README, the
// guides and the CLI reference joined into one file, so an agent can take in
// the whole manual with one fetch. Out of context a relative link points
// nowhere, so every one is rewritten to its GitHub URL.

const (
	// rawBaseURL serves a file's bytes, which is what an agent following a link
	// wants; llms.txt links the same way.
	rawBaseURL = "https://raw.githubusercontent.com/germanamz/tusk/main/"
	// treeBaseURL lists a directory, which raw.githubusercontent.com can't.
	treeBaseURL = "https://github.com/germanamz/tusk/tree/main/"
)

const llmsFullHeader = `# Tusk documentation

Every page of tusk's user docs in one file: the README, the guides in docs/guide/ and the CLI reference in docs/cli/. Each page starts with a heading that names its path, and its relative links point at GitHub. ` + "`make docs`" + ` writes this file from those pages, so edit them rather than this. llms.txt indexes the same pages.
`

// llmsGuideOrder is the reading order of docs/guide/ in llms-full.txt, the
// order llms.txt lists the guides in. A guide missing from it fails `make
// docs`, so a new page gets a deliberate place here, and a reminder that
// llms.txt and the README list the guides too.
var llmsGuideOrder = []string{
	"install.md",
	"workspace.md",
	"querying.md",
	"rules-and-doctor.md",
	"mcp.md",
	"benchmarks.md",
}

var (
	// markdownDestinationPattern finds an inline link or image destination:
	// the run after "](", past an optional "<", up to whitespace or the close.
	markdownDestinationPattern = regexp.MustCompile(`\]\([ \t]*<?([^\s)>]+)`)
	// referenceDefinitionPattern finds a link reference definition's
	// destination: `[label]: destination`.
	referenceDefinitionPattern = regexp.MustCompile(`(?m)^ {0,3}\[[^\]]+\]:[ \t]*<?([^\s>]+)`)
	htmlTagPattern             = regexp.MustCompile(`<[A-Za-z][^>]*>`)
	// htmlURLAttrPattern finds a quoted href, src or srcset value in a tag.
	htmlURLAttrPattern = regexp.MustCompile(`(?i)\s(?:href|src|srcset)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

// writeLLMsFull writes llms-full.txt to outPath. The file's directory is the
// repo root: README.md and docs/guide/ are read from it, and cliDir, the
// freshly generated CLI reference, must sit inside it. commandPages are the
// generated pages in index order.
func writeLLMsFull(outPath, cliDir string, commandPages []string) error {
	root, rootErr := filepath.Abs(filepath.Dir(outPath))

	if rootErr != nil {
		return fmt.Errorf("resolve llms-full root: %w", rootErr)
	}

	absCLIDir, cliErr := filepath.Abs(cliDir)

	if cliErr != nil {
		return fmt.Errorf("resolve markdown dir: %w", cliErr)
	}

	cliRel, relErr := filepath.Rel(root, absCLIDir)

	if relErr != nil || !filepath.IsLocal(cliRel) {
		return fmt.Errorf("markdown dir %s must be inside %s, the directory llms-full.txt is written to", cliDir, root)
	}

	full, buildErr := buildLLMsFull(root, filepath.ToSlash(cliRel), commandPages)

	if buildErr != nil {
		return buildErr
	}

	if writeErr := os.WriteFile(outPath, full, 0o644); writeErr != nil {
		return fmt.Errorf("write llms-full: %w", writeErr)
	}

	return nil
}

// buildLLMsFull joins the docs under root into llms-full.txt: the README, the
// guides in llmsGuideOrder, then the CLI reference in cliDir (a root-relative,
// slash-separated path) starting with its index, then its hand-written pages
// by name, then commandPages. Each page's frontmatter is dropped and its
// relative links made absolute.
func buildLLMsFull(root, cliDir string, commandPages []string) ([]byte, error) {
	guides, guideErr := guidePages(root)

	if guideErr != nil {
		return nil, guideErr
	}

	references, cliErr := cliPages(root, cliDir, commandPages)

	if cliErr != nil {
		return nil, cliErr
	}

	pages := append(append([]string{"README.md"}, guides...), references...)

	isDir := func(target string) bool {
		info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(target)))

		return statErr == nil && info.IsDir()
	}

	var out bytes.Buffer

	out.WriteString(llmsFullHeader)

	for _, page := range pages {
		content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(page)))

		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", page, readErr)
		}

		body := bytes.TrimRight(bytes.TrimLeft(stripFrontmatter(content), "\n"), " \t\n")

		fmt.Fprintf(&out, "\n# %s\n\nSource: %s%s\n\n", page, rawBaseURL, escapePath(page))
		out.Write(rewriteRelativeLinks(page, body, isDir))
		out.WriteByte('\n')
	}

	return out.Bytes(), nil
}

// guidePages returns the root-relative guide paths in llmsGuideOrder, failing
// on a guide the order leaves out.
func guidePages(root string) ([]string, error) {
	entries, readErr := os.ReadDir(filepath.Join(root, "docs", "guide"))

	if readErr != nil {
		return nil, fmt.Errorf("read guides: %w", readErr)
	}

	listed := make(map[string]bool, len(llmsGuideOrder))

	for _, guide := range llmsGuideOrder {
		listed[guide] = true
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || listed[entry.Name()] {
			continue
		}

		return nil, fmt.Errorf("docs/guide/%s is not in llmsGuideOrder (cmd/tusk/docgen_llms.go): add it where it belongs in the reading order", entry.Name())
	}

	pages := make([]string, 0, len(llmsGuideOrder))

	for _, guide := range llmsGuideOrder {
		pages = append(pages, "docs/guide/"+guide)
	}

	return pages, nil
}

// cliPages returns the CLI reference's root-relative paths: the index, the
// hand-written pages beside it by name, then commandPages.
func cliPages(root, cliDir string, commandPages []string) ([]string, error) {
	entries, readErr := os.ReadDir(filepath.Join(root, filepath.FromSlash(cliDir)))

	if readErr != nil {
		return nil, fmt.Errorf("read CLI reference: %w", readErr)
	}

	generated := make(map[string]bool, len(commandPages)+1)

	generated["README.md"] = true

	for _, page := range commandPages {
		generated[page] = true
	}

	pages := []string{cliDir + "/README.md"}

	// ReadDir sorts by name.
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || generated[entry.Name()] {
			continue
		}

		pages = append(pages, cliDir+"/"+entry.Name())
	}

	for _, page := range commandPages {
		pages = append(pages, cliDir+"/"+page)
	}

	return pages, nil
}

// stripFrontmatter drops a leading YAML frontmatter block, which the
// generated command pages carry for site generators.
func stripFrontmatter(content []byte) []byte {
	rest, opened := bytes.CutPrefix(content, []byte("---\n"))

	if !opened {
		return content
	}

	if _, after, closed := bytes.Cut(rest, []byte("\n---\n")); closed {
		return after
	}

	return content
}

// rewriteRelativeLinks returns body, the page at sourcePath (root-relative),
// with every relative link, image and HTML href/src/srcset destination
// replaced by its GitHub URL: a directory's tree page when the destination
// ends in "/" or isDir reports one, the raw file otherwise. An in-page anchor
// points at the page itself. Absolute URLs, destinations above the root and
// anything inside code are left alone; code is found the way the index finds
// it (wikilink.MaskCode), and edits land at the same offsets in body.
func rewriteRelativeLinks(sourcePath string, body []byte, isDir func(string) bool) []byte {
	masked := wikilink.MaskCode(body)

	type edit struct {
		start, end  int
		replacement string
	}

	var edits []edit

	resolve := func(destination string) (string, bool) {
		return absoluteURL(sourcePath, destination, isDir)
	}

	for _, pattern := range []*regexp.Regexp{markdownDestinationPattern, referenceDefinitionPattern} {
		for _, match := range pattern.FindAllSubmatchIndex(masked, -1) {
			if absolute, ok := resolve(string(body[match[2]:match[3]])); ok {
				edits = append(edits, edit{start: match[2], end: match[3], replacement: absolute})
			}
		}
	}

	for _, tag := range htmlTagPattern.FindAllIndex(masked, -1) {
		for _, attr := range htmlURLAttrPattern.FindAllSubmatchIndex(masked[tag[0]:tag[1]], -1) {
			start, end := attr[2], attr[3]

			if start < 0 {
				start, end = attr[4], attr[5]
			}

			start += tag[0]
			end += tag[0]

			value := string(body[start:end])
			replacement := rewriteSrcset(value, resolve)

			if replacement != value {
				edits = append(edits, edit{start: start, end: end, replacement: replacement})
			}
		}
	}

	sort.Slice(edits, func(left, right int) bool {
		return edits[left].start < edits[right].start
	})

	var out bytes.Buffer

	cursor := 0

	for _, change := range edits {
		if change.start < cursor {
			continue
		}

		out.Write(body[cursor:change.start])
		out.WriteString(change.replacement)

		cursor = change.end
	}

	out.Write(body[cursor:])

	return out.Bytes()
}

// rewriteSrcset resolves each URL in a comma-separated list of "url
// [descriptor]" candidates, keeping the descriptors and spacing. An href or
// src value is a list of one.
func rewriteSrcset(value string, resolve func(string) (string, bool)) string {
	candidates := strings.Split(value, ",")

	for index, candidate := range candidates {
		trimmed := strings.TrimLeft(candidate, " \t\n")
		lead := candidate[:len(candidate)-len(trimmed)]

		urlEnd := strings.IndexAny(trimmed, " \t\n")

		if urlEnd < 0 {
			urlEnd = len(trimmed)
		}

		if absolute, ok := resolve(trimmed[:urlEnd]); ok {
			candidates[index] = lead + absolute + trimmed[urlEnd:]
		}
	}

	return strings.Join(candidates, ",")
}

// absoluteURL resolves a destination written on the page at sourcePath to its
// GitHub URL, keeping any query and fragment as written. It returns false for
// an absolute or protocol-relative URL, an empty destination and one that
// climbs above the root (see pathref.Link).
func absoluteURL(sourcePath, destination string, isDir func(string) bool) (string, bool) {
	parsed, parseErr := url.Parse(destination)

	if parseErr != nil || parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(destination, "//") {
		return "", false
	}

	suffix := ""

	if cut := strings.IndexAny(destination, "?#"); cut >= 0 {
		suffix = destination[cut:]
	}

	if parsed.Path == "" {
		if suffix == "" {
			return "", false
		}

		return rawBaseURL + escapePath(sourcePath) + suffix, true
	}

	target, resolved := pathref.Link(sourcePath, destination)

	if !resolved {
		return "", false
	}

	if strings.HasSuffix(parsed.Path, "/") || isDir(target) {
		return treeBaseURL + escapePath(target) + suffix, true
	}

	return rawBaseURL + escapePath(target) + suffix, true
}

// escapePath percent-encodes a slash-separated path for a URL.
func escapePath(target string) string {
	return (&url.URL{Path: target}).EscapedPath()
}
