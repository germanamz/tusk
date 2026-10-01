package claudeplugin

import (
	"fmt"
	"strings"
)

// DefaultMaxBytes is the context block's budget when tusk.toml sets no
// [context] max-bytes: about four thousand tokens.
const DefaultMaxBytes = 16384

// Section is one digest section of the context block. Items is how many
// pieces Render can be cut down to (nodes, for pinned and recent); a Whole
// section is kept or dropped as a unit and always renders all its Items.
type Section struct {
	Heading string
	Items   int
	Whole   bool
	// Render returns the section body holding its first count items; Fit
	// calls it with count == Items for the full body.
	Render func(count int) (string, error)
}

// Fit assembles the context block: the orientation, then each section under
// a "## <Heading>" line, within budget bytes. The orientation is never cut.
// Sections are listed highest priority first; over budget, Fit cuts from the
// last one back, dropping Whole sections outright and trimming the others a
// whole item at a time, and ends the block with a line saying so.
func Fit(orientation string, sections []Section, budget int) (string, error) {
	kept := make([]int, len(sections))

	for index, section := range sections {
		kept[index] = section.Items

		if section.Whole {
			kept[index] = 1
		}
	}

	block, assembleErr := assemble(orientation, sections, kept)

	if assembleErr != nil || len(block) <= budget {
		return block, assembleErr
	}

	footer := fmt.Sprintf("\n… digest truncated at %d bytes; call tusk_context for the full digest.\n", budget)

	for index := len(sections) - 1; index >= 0; index-- {
		for kept[index] > 0 {
			if sections[index].Whole {
				kept[index] = 0
			} else {
				kept[index]--
			}

			block, assembleErr = assemble(orientation, sections, kept)

			if assembleErr != nil {
				return "", assembleErr
			}

			if len(block)+len(footer) <= budget {
				return block + footer, nil
			}
		}
	}

	return block + footer, nil
}

// assemble renders the orientation and every section with something kept,
// each under its heading. kept counts items, or is 1 or 0 for a Whole
// section. A section whose body renders empty is left out, heading and all.
func assemble(orientation string, sections []Section, kept []int) (string, error) {
	var builder strings.Builder

	builder.WriteString(orientation)

	for index, section := range sections {
		if kept[index] == 0 {
			continue
		}

		count := kept[index]

		if section.Whole {
			count = section.Items
		}

		body, renderErr := section.Render(count)

		if renderErr != nil {
			return "", fmt.Errorf("render %s: %w", section.Heading, renderErr)
		}

		if strings.TrimSpace(body) == "" {
			continue
		}

		builder.WriteString("\n## " + section.Heading + "\n")
		builder.WriteString(strings.TrimRight(body, "\n") + "\n")
	}

	return builder.String(), nil
}
