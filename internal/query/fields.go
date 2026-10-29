package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// fieldNames are the row keys a fields projection can name, in the order the
// error message lists them. properties.<name> keeps one property; "units" is
// accepted as matched_units, the key it means (IncludeFromFields takes both).
var fieldNames = []string{
	"id", "type", "path", "title", "parent_id", "score", "snippet",
	"body", "properties", "edges", "paths", "matched_units",
}

// propertyFieldPrefix names one property in a fields projection.
const propertyFieldPrefix = "properties."

// explainKeys are the score-trace keys a semantic row carries when the caller
// asked to explain the ranking. A projection keeps them without being told:
// explain is its own explicit ask.
var explainKeys = []string{
	"cosine_score", "lexical_score", "title_score", "graph_score", "weight", "final_score", "distance",
}

// Fields is a parsed fields projection (#851): the row keys a read verb's
// JSON keeps, in the order the caller named them, and the properties it
// keeps when it named them one by one. The zero value keeps every key.
type Fields struct {
	names         []string
	keys          []string
	properties    []string
	allProperties bool
}

// ParseFields parses a fields list into a Fields. Unknown names are rejected
// with an error that lists the valid ones; empty names and duplicates are
// dropped. Every read verb (query, node list, node get) and every transport
// validates through it, whatever format renders the rows.
func ParseFields(raw []string) (Fields, error) {
	var fields Fields

	for _, name := range raw {
		if name == "units" {
			name = "matched_units"
		}

		if name == "" || slices.Contains(fields.names, name) {
			continue
		}

		key := name

		switch {
		case strings.HasPrefix(name, propertyFieldPrefix):
			property := strings.TrimPrefix(name, propertyFieldPrefix)

			if property == "" {
				return Fields{}, fmt.Errorf("field %q names no property (use properties.<name>)", name)
			}

			fields.properties = append(fields.properties, property)
			key = "properties"
		case name == "properties":
			fields.allProperties = true
		case !slices.Contains(fieldNames, name):
			return Fields{}, fmt.Errorf("unknown field %q (valid: %s, properties.<name>)", name, strings.Join(fieldNames, ", "))
		}

		fields.names = append(fields.names, name)

		if !slices.Contains(fields.keys, key) {
			fields.keys = append(fields.keys, key)
		}
	}

	return fields, nil
}

// Any reports whether the projection names at least one field.
func (fields Fields) Any() bool {
	return len(fields.names) > 0
}

// Names returns the parsed field names in the order given, with "units"
// spelled matched_units. Compact renderers take this list.
func (fields Fields) Names() []string {
	return slices.Clone(fields.names)
}

// Has reports whether the row key is kept: always with no projection.
func (fields Fields) Has(key string) bool {
	return !fields.Any() || slices.Contains(fields.keys, key)
}

// KeepsProperty reports whether the named property is kept: every property
// is when the projection keeps "properties" whole or names no field.
func (fields Fields) KeepsProperty(name string) bool {
	return !fields.Any() || fields.allProperties || slices.Contains(fields.properties, name)
}

// Include returns the expansions the named fields need loaded.
func (fields Fields) Include() IncludeSet {
	return IncludeFromFields(fields.keys)
}

// Project returns row's JSON with only the kept keys, in the order named. The
// row is marshaled first, so a row type's own wire shape (ScoredRow's rounded
// scores) is what gets projected. matched_units brings units_total, and the
// explain keys stay whenever the row carries them. A key the row doesn't carry
// is left out; so is properties when none of the named properties is set.
func (fields Fields) Project(row any) (json.RawMessage, error) {
	encoded, marshalErr := json.Marshal(row)

	if marshalErr != nil {
		return nil, marshalErr
	}

	if !fields.Any() {
		return encoded, nil
	}

	var object map[string]json.RawMessage

	if unmarshalErr := json.Unmarshal(encoded, &object); unmarshalErr != nil {
		return nil, fmt.Errorf("fields: project %T: %w", row, unmarshalErr)
	}

	keys := make([]string, 0, len(fields.keys)+len(explainKeys))

	for _, key := range fields.keys {
		keys = append(keys, key)

		if key == "matched_units" {
			keys = append(keys, "units_total")
		}
	}

	keys = append(keys, explainKeys...)

	var out bytes.Buffer

	out.WriteByte('{')

	for _, key := range keys {
		value, present := object[key]

		if !present {
			continue
		}

		if key == "properties" && !fields.allProperties {
			trimmed, trimErr := fields.trimProperties(value)

			if trimErr != nil {
				return nil, trimErr
			}

			if trimmed == nil {
				continue
			}

			value = trimmed
		}

		if out.Len() > 1 {
			out.WriteByte(',')
		}

		encodedKey, _ := json.Marshal(key)

		out.Write(encodedKey)
		out.WriteByte(':')
		out.Write(value)
	}

	out.WriteByte('}')

	return out.Bytes(), nil
}

// trimProperties keeps the named properties of a properties object, or
// returns nil when it holds none of them.
func (fields Fields) trimProperties(value json.RawMessage) (json.RawMessage, error) {
	var properties map[string]json.RawMessage

	if unmarshalErr := json.Unmarshal(value, &properties); unmarshalErr != nil {
		return nil, fmt.Errorf("fields: project properties: %w", unmarshalErr)
	}

	kept := map[string]json.RawMessage{}

	for _, name := range fields.properties {
		if property, present := properties[name]; present {
			kept[name] = property
		}
	}

	if len(kept) == 0 {
		return nil, nil
	}

	return json.Marshal(kept)
}

// ProjectRow returns row ready for JSON output: row itself when fields names
// nothing, else a value that marshals as Fields.Project(row). Marshaling it
// reports a projection error, so envelopes that hold it need no error path.
func ProjectRow(fields Fields, row any) any {
	if !fields.Any() {
		return row
	}

	return projectedRow{fields: fields, row: row}
}

// ProjectRows is ProjectRow for a slice: rows itself, or a value that marshals
// as the array of each row projected.
func ProjectRows[Item any](fields Fields, rows []Item) any {
	if !fields.Any() {
		return rows
	}

	projected := make([]projectedRow, 0, len(rows))

	for _, row := range rows {
		projected = append(projected, projectedRow{fields: fields, row: row})
	}

	return projected
}

// projectedRow is a row that marshals through its projection.
type projectedRow struct {
	fields Fields
	row    any
}

// MarshalJSON writes the projected row.
func (projected projectedRow) MarshalJSON() ([]byte, error) {
	return projected.fields.Project(projected.row)
}
