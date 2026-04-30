package syntax

import "strings"

// FilterSet is a collection of parsed inline syntax terms, implicitly AND'd.
// Used by filter expressions and task creation/modification commands.
type FilterSet struct {
	Fields []FieldFilter
	Tags   []TagFilter
	Text   []string // free text tokens (joined as title when used in add)
}

// HasField returns true if the FilterSet contains a field with the given key.
func (fs *FilterSet) HasField(key string) bool {
	for _, field := range fs.Fields {
		if field.Key == key {
			return true
		}
	}
	return false
}

// GetField returns the first FieldFilter with the given key.
// The bool is false if no field with that key exists.
func (fs *FilterSet) GetField(key string) (FieldFilter, bool) {
	for _, field := range fs.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return FieldFilter{}, false
}

// IncludeTags returns the names of all non-excluded tags.
func (fs *FilterSet) IncludeTags() []string {
	var out []string
	for _, tag := range fs.Tags {
		if !tag.Exclude {
			out = append(out, tag.Name)
		}
	}
	return out
}

// ExcludeTags returns the names of all excluded tags.
func (fs *FilterSet) ExcludeTags() []string {
	var out []string
	for _, tag := range fs.Tags {
		if tag.Exclude {
			out = append(out, tag.Name)
		}
	}
	return out
}

// Title joins free text tokens into a single string.
func (fs *FilterSet) Title() string {
	return strings.Join(fs.Text, " ")
}

// FieldFilter represents a key=value term.
//
// Modifier carries any registered prefix rune recognised by the lexer
// (e.g. '+', '-'). 0 means "no modifier". The syntax package attaches no
// semantics — consumers interpret it however they like (include/exclude,
// add/remove, numeric delta, ...).
type FieldFilter struct {
	Key      string // field name (e.g. "status", "project", "uda.env")
	Value    string // raw value string, unparsed
	Modifier byte   // registered prefix marker ('+' / '-' / ...); 0 if none
	Pos      int    // byte offset in input
}

// TagFilter represents +tag or -tag.
type TagFilter struct {
	Name    string
	Exclude bool // true for -tag, false for +tag
	Pos     int
}
