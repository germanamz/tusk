package claudeplugin

import (
	"fmt"
	"strings"
)

// DefaultMaxRefPages caps how many pages the edit reminder lists, so a path
// that every page names doesn't flood the tool result.
const DefaultMaxRefPages = 5

// RefPage is one page that names an edited path, as the edit reminder shows it.
type RefPage struct {
	ID    string
	Title string
	// Named is the ref target that matched: the edited path itself, or a
	// directory above it.
	Named string
	// Where says where the page names it ("lines 40-41 (Core service)"); ""
	// when the format keeps no positions.
	Where string
}

// Refs renders the reminder the plugin hook adds after an edit to path: which
// pages name it, where, and how to list the rest. pages are ordered most
// specific first; at most limit are listed (DefaultMaxRefPages when limit is
// not positive). It returns "" when no page names the path.
func Refs(path string, pages []RefPage, limit int) string {
	if len(pages) == 0 {
		return ""
	}

	if limit <= 0 {
		limit = DefaultMaxRefPages
	}

	var builder strings.Builder

	noun := "pages name"

	if len(pages) == 1 {
		noun = "page names"
	}

	fmt.Fprintf(&builder, "tusk: %d %s %s. Check whether this edit changes what they say.\n", len(pages), noun, path)

	for _, page := range pages[:min(limit, len(pages))] {
		line := "- " + page.ID

		if page.Title != "" {
			line += fmt.Sprintf(" %q", page.Title)
		}

		var details []string

		if page.Where != "" {
			details = append(details, page.Where)
		}

		if page.Named != path {
			details = append(details, "via "+page.Named+"/")
		}

		if len(details) > 0 {
			line += ": " + strings.Join(details, ", ")
		}

		builder.WriteString(line + "\n")
	}

	if rest := len(pages) - limit; rest > 0 {
		fmt.Fprintf(&builder, "...and %d more: tusk_query names-path=%s\n", rest, path)
	}

	return builder.String()
}
