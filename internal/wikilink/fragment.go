package wikilink

import (
	"strings"
	"unicode"
)

// A target's #fragment names a sub-unit of the file its path part names
// (#808), in one of four forms:
//
//   - a structural address, `[[auth-rfc#S1.2]]`: the sub-unit id itself. It
//     wins whenever the file has that sub-unit, so a link that resolved before
//     heading lookup existed resolves the same way.
//   - heading text, `[[auth-rfc#Some heading]]`: Obsidian's heading link,
//     matched against the file's section headings by HeadingKey.
//   - a heading chain, `[[auth-rfc#Parent#Child]]`: each heading is looked for
//     inside the one before it.
//   - a block reference, `[[auth-rfc#^block-id]]`: Obsidian's link to the
//     paragraph, list item or block a `^block-id` marker names, matched by
//     BlockKey (#819).
//
// An empty path part, `[[#Some heading]]`, names the linking file itself. A
// fragment that names nothing in an existing file leaves the link on the file
// and reports the fragment missing, which doctor surfaces.

// HeadingKey folds heading text for comparison, the way Obsidian matches a
// heading link: letters and digits are kept in lower case, every other rune
// separates words, and a run of separators is one space. `Foo: Bar`, `foo bar`
// and the GitHub-style `foo-bar` share a key. It is what nodes.name_key stores
// for a section row.
func HeadingKey(text string) string {
	var builder strings.Builder

	separated := false

	for _, char := range text {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && !unicode.IsMark(char) {
			separated = true

			continue
		}

		if separated && builder.Len() > 0 {
			builder.WriteByte(' ')
		}

		separated = false
		builder.WriteRune(unicode.ToLower(char))
	}

	return builder.String()
}

// IsBlockRef reports whether fragment (with its leading `#`) is an Obsidian
// block reference, `#^block-id`.
func IsBlockRef(fragment string) bool {
	return strings.HasPrefix(fragment, "#^")
}

// BlockKey is the key a block id is compared by: the id in lower case behind
// a `^`, so a block reference matches whatever case either side is written
// in, as a heading link does. It is what nodes.name_key stores for a sub-unit
// a `^block-id` marker names, and no HeadingKey can start with `^`.
func BlockKey(id string) string {
	return "^" + strings.ToLower(id)
}

// NeedsLookup reports whether the node target names can only be found by
// looking it up, so its edge keeps the link text and is re-resolved as the
// vault changes: a name, or any #fragment, whose heading may move or be
// renamed.
func NeedsLookup(target string) bool {
	if IsName(target) {
		return true
	}

	_, fragment := SplitFragment(target)

	return fragment != ""
}

// Section is one heading of a file, as a fragment is matched against it.
type Section struct {
	// ID is the section's sub-unit id, `notes/auth-rfc#S1.2`.
	ID string
	// Parent is the id of the section it sits in, or the file id at the top.
	Parent string
	// Key is the HeadingKey of its heading.
	Key string
}

// MatchHeading returns the id of the section a heading fragment names among
// sections, one file's in document order. chain is the fragment without its
// leading `#`: one heading, or `Parent#Child`. Each heading is the first
// section whose key matches, below the section matched before it (anywhere in
// the file for the first), so a heading several sections share resolves to the
// first one, as in Obsidian. Steps with no letters or digits are skipped.
func MatchHeading(chain string, sections []Section) (string, bool) {
	parents := make(map[string]string, len(sections))

	for _, section := range sections {
		parents[section.ID] = section.Parent
	}

	matched := -1

	for step := range strings.SplitSeq(chain, "#") {
		key := HeadingKey(step)

		if key == "" {
			continue
		}

		next := -1

		for index := matched + 1; index < len(sections); index++ {
			if sections[index].Key != key {
				continue
			}

			if matched >= 0 && !within(parents, sections[index].ID, sections[matched].ID) {
				continue
			}

			next = index

			break
		}

		if next < 0 {
			return "", false
		}

		matched = next
	}

	if matched < 0 {
		return "", false
	}

	return sections[matched].ID, true
}

// within reports whether section id sits somewhere below ancestor.
func within(parents map[string]string, id, ancestor string) bool {
	for parent, known := parents[id]; known; parent, known = parents[parent] {
		if parent == ancestor {
			return true
		}
	}

	return false
}

// Anchors answers what a fragment can name inside a file.
type Anchors interface {
	// Exists reports whether a node has id: a file, or a sub-unit.
	Exists(id string) (bool, error)
	// Sections returns fileID's sections in document order.
	Sections(fileID string) ([]Section, error)
	// Blocks maps the BlockKey of each block id in fileID to the sub-unit it
	// names, the first in document order when several units share an id.
	Blocks(fileID string) (map[string]string, error)
}

// Lookup is everything ResolveTarget consults: file names and the anchors
// inside files.
type Lookup interface {
	Files
	Anchors
}

// Source is the file a link is written in.
type Source struct {
	// ID is its node id, which a link with no path part (`[[#Heading]]`)
	// names.
	ID string
	// Path is its workspace-relative path, which relative links resolve from.
	Path string
}

// Target is what a link target resolves to.
type Target struct {
	// ID is the node the link names: the file, or the sub-unit its fragment
	// names. Empty when the link names no file.
	ID string
	// Candidates lists the files a name matched when it matched several.
	Candidates []string
	// MissingFragment reports a fragment that names no sub-unit, heading or
	// block of the file, which ID then names instead.
	MissingFragment bool
}

// ResolveTarget returns the node target names when it is linked from source:
// the file its path part names (Resolve's rule, and source itself for an
// empty path part), then the sub-unit its fragment names in that file. A
// target whose file does not exist resolves to no id.
func ResolveTarget(target string, source Source, lookup Lookup) (Target, error) {
	pathPart, fragment := SplitFragment(target)
	fileID := source.ID

	if pathPart != "" {
		id, candidates, resolveErr := Resolve(pathPart, source.Path, lookup)

		if resolveErr != nil || id == "" {
			return Target{Candidates: candidates}, resolveErr
		}

		fileID = id
	}

	if fileID == "" {
		return Target{}, nil
	}

	fileExists, fileErr := lookup.Exists(fileID)

	if fileErr != nil || !fileExists {
		return Target{}, fileErr
	}

	if fragment == "" || fragment == "#" {
		return Target{ID: fileID}, nil
	}

	// No structural address starts with `^`, so a block reference is only
	// ever a block id.
	if IsBlockRef(fragment) {
		blocks, blocksErr := lookup.Blocks(fileID)

		if blocksErr != nil {
			return Target{}, blocksErr
		}

		if blockID, found := blocks[BlockKey(fragment[2:])]; found {
			return Target{ID: blockID}, nil
		}

		return Target{ID: fileID, MissingFragment: true}, nil
	}

	subUnitExists, subUnitErr := lookup.Exists(fileID + fragment)

	if subUnitErr != nil {
		return Target{}, subUnitErr
	}

	if subUnitExists {
		return Target{ID: fileID + fragment}, nil
	}

	sections, sectionsErr := lookup.Sections(fileID)

	if sectionsErr != nil {
		return Target{}, sectionsErr
	}

	if sectionID, found := MatchHeading(fragment[1:], sections); found {
		return Target{ID: sectionID}, nil
	}

	return Target{ID: fileID, MissingFragment: true}, nil
}
