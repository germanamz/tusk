package doctor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/pathref"
)

// IssuePathMissing surfaces a workspace path a page names in inline code (a
// path ref, from an edge type with paths = true) that is not on disk: the file
// was renamed or deleted after the page was written, or the page names one
// that doesn't exist yet. NodeID is the page; Locations are the innermost
// sections holding the mentions. Only an anchored ref is checked, one whose
// first segment exists at the workspace root, so a span that never named a
// path (`application/json`) stays quiet. A warning, not an error: a plan that
// names a file still to be written is legitimate.
const IssuePathMissing = "path-missing"

// pathMissingKey groups one page's mentions of one missing path under one edge
// type into a single finding.
type pathMissingKey struct {
	sourceID string
	edgeType string
	target   string
}

// checkPathRefs flags every anchored path ref whose path is gone, comparing
// each segment's case exactly so macOS and Linux agree. No-op without the edge
// repo or the workspace root.
func checkPathRefs(config Config) ([]Issue, error) {
	if config.Edges == nil || config.Root == "" {
		return nil, nil
	}

	refs, listErr := config.Edges.AllPathRefs()

	if listErr != nil {
		return nil, listErr
	}

	disk := pathref.NewDisk(config.Root)
	missing := map[string]bool{}
	groups := map[pathMissingKey]int{}
	lines := map[pathMissingKey][]int{}
	sections := map[string][]index.SectionSpan{}

	var issues []Issue

	for _, ref := range refs {
		gone, checked := missing[ref.Target]

		if !checked {
			gone = disk.Anchored(ref.Target) && !disk.Present(ref.Target)
			missing[ref.Target] = gone
		}

		if !gone {
			continue
		}

		key := pathMissingKey{sourceID: ref.SourceID, edgeType: ref.Type, target: ref.Target}
		position, seen := groups[key]

		if !seen {
			position = len(issues)
			groups[key] = position
			issues = append(issues, Issue{Kind: IssuePathMissing, NodeID: ref.SourceID})
		}

		if ref.Line <= 0 {
			continue
		}

		lines[key] = append(lines[key], ref.Line)

		section, sectionErr := innermostSectionID(config, sections, ref.SourceID, ref.Line)

		if sectionErr != nil {
			return nil, sectionErr
		}

		if section != "" && !slices.Contains(issues[position].Locations, section) {
			issues[position].Locations = append(issues[position].Locations, section)
		}
	}

	for key, position := range groups {
		issues[position].Message = pathMissingMessage(key, lines[key])
	}

	return issues, nil
}

// innermostSectionID returns the id of the innermost section of sourceID that
// holds line, or "" when there is none or no database to ask. Section spans are
// loaded once per page.
func innermostSectionID(config Config, cache map[string][]index.SectionSpan, sourceID string, line int) (string, error) {
	if config.DB == nil {
		return "", nil
	}

	spans, cached := cache[sourceID]

	if !cached {
		loaded, loadErr := index.SectionSpans(config.DB, sourceID)

		if loadErr != nil {
			return "", loadErr
		}

		cache[sourceID] = loaded
		spans = loaded
	}

	section, found := index.InnermostSection(spans, line)

	if !found {
		return "", nil
	}

	return section.ID, nil
}

func pathMissingMessage(key pathMissingKey, lines []int) string {
	where := ""

	switch len(lines) {
	case 0:
	case 1:
		where = " (line " + strconv.Itoa(lines[0]) + ")"
	default:
		rendered := make([]string, len(lines))

		for position, line := range lines {
			rendered[position] = strconv.Itoa(line)
		}

		where = " (lines " + strings.Join(rendered, ", ") + ")"
	}

	return fmt.Sprintf("%s names %q%s, which is not on disk", key.edgeType, key.target, where)
}
