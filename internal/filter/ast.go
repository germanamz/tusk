package filter

import "strings"

// FilterSet is the root AST node — a collection of filter terms, implicitly AND'd.
// Designed to later wrap a BoolExpr node for OR/NOT/grouping.
type FilterSet struct {
	Fields []FieldFilter
	Tags   []TagFilter
	Text   []string // free text tokens (joined as title when used in add)
}

// HasField returns true if the FilterSet contains a field with the given key.
func (fs *FilterSet) HasField(key string) bool {
	for _, f := range fs.Fields {
		if f.Key == key {
			return true
		}
	}
	return false
}

// GetField returns the first FieldFilter with the given key, or false if not found.
func (fs *FilterSet) GetField(key string) (FieldFilter, bool) {
	for _, f := range fs.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return FieldFilter{}, false
}

// IncludeTags returns the names of all non-excluded tags.
func (fs *FilterSet) IncludeTags() []string {
	var out []string
	for _, t := range fs.Tags {
		if !t.Exclude {
			out = append(out, t.Name)
		}
	}
	return out
}

// ExcludeTags returns the names of all excluded tags.
func (fs *FilterSet) ExcludeTags() []string {
	var out []string
	for _, t := range fs.Tags {
		if t.Exclude {
			out = append(out, t.Name)
		}
	}
	return out
}

// Title joins free text tokens into a single string.
func (fs *FilterSet) Title() string {
	return strings.Join(fs.Text, " ")
}

// FieldFilter represents a key:value or key:min..max term.
type FieldFilter struct {
	Key   string // "status", "project", "priority", "due", "parent", "tree", "waiting"
	Value string // raw value string, unparsed
	Pos   int    // byte offset in input
}

// TagFilter represents +tag or -tag.
type TagFilter struct {
	Name    string
	Exclude bool // true for -tag, false for +tag
	Pos     int
}
