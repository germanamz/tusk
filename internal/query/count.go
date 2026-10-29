package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// errCountSemantic rejects a count on a semantic query: a ranking has no
// total to report beyond the page it returns.
var errCountSemantic = errors.New("count and group-by count a structural filter; drop semantic")

// CountResult is a count query's payload (#851): how many rows the filter
// matched, and, with GroupBy set, how many fall under each value of it.
type CountResult struct {
	Total   int
	GroupBy string
	Groups  []CountGroup
}

// CountGroup is one group-by value and how many matches carry it. A nil Value
// groups the matches that have no value: the property is missing, null or an
// empty list.
type CountGroup struct {
	Value any `json:"value"`
	Count int `json:"count"`
}

// MarshalJSON writes {"count": N}, plus group_by and its groups (an empty
// array when nothing matched) when the count was grouped.
func (result CountResult) MarshalJSON() ([]byte, error) {
	if result.GroupBy == "" {
		return json.Marshal(struct {
			Total int `json:"count"`
		}{result.Total})
	}

	groups := result.Groups

	if groups == nil {
		groups = []CountGroup{}
	}

	return json.Marshal(struct {
		Total   int          `json:"count"`
		GroupBy string       `json:"group_by"`
		Groups  []CountGroup `json:"groups"`
	}{result.Total, result.GroupBy, groups})
}

// validateGroupBy accepts type and any property name. id, path and title
// tell nodes apart rather than group them, so grouping by one is a mistake.
func validateGroupBy(key string) error {
	switch key {
	case "id", "path", "title":
		return fmt.Errorf("group-by %q lists nodes one by one; group by type or a property", key)
	}

	return nil
}

// countRows counts rows, grouped by groupBy when it is set. A node counts
// once in each group it falls under (groupValues). Groups are ordered by
// count, largest first; on a tie the no-value group comes last and the rest
// sort by value.
func countRows(rows []Row, groupBy string) (*CountResult, error) {
	result := &CountResult{Total: len(rows), GroupBy: groupBy}

	if groupBy == "" {
		return result, nil
	}

	byKey := map[string]*CountGroup{}

	for _, row := range rows {
		values, valuesErr := groupValues(row, groupBy)

		if valuesErr != nil {
			return nil, valuesErr
		}

		for _, value := range values {
			key := groupKey(value)
			group, seen := byKey[key]

			if !seen {
				group = &CountGroup{Value: value}
				byKey[key] = group
			}

			group.Count++
		}
	}

	result.Groups = make([]CountGroup, 0, len(byKey))

	for _, group := range byKey {
		result.Groups = append(result.Groups, *group)
	}

	sort.Slice(result.Groups, func(left, right int) bool {
		leftGroup, rightGroup := result.Groups[left], result.Groups[right]

		if leftGroup.Count != rightGroup.Count {
			return leftGroup.Count > rightGroup.Count
		}

		if (leftGroup.Value == nil) != (rightGroup.Value == nil) {
			return rightGroup.Value == nil
		}

		return groupKey(leftGroup.Value) < groupKey(rightGroup.Value)
	})

	return result, nil
}

// groupValues returns the values row falls under: its type, a property's
// value, or each distinct item of a list property. A missing, null or empty
// list property is the single nil value.
func groupValues(row Row, groupBy string) ([]any, error) {
	if groupBy == "type" {
		return []any{row.Type}, nil
	}

	properties, propErr := unmarshalProperties(row.PropertiesRaw, row.ID)

	if propErr != nil {
		return nil, propErr
	}

	value := properties[groupBy]
	items, isList := value.([]any)

	if !isList {
		return []any{value}, nil
	}

	if len(items) == 0 {
		return []any{nil}, nil
	}

	distinct := make([]any, 0, len(items))
	seen := map[string]bool{}

	for _, item := range items {
		if key := groupKey(item); !seen[key] {
			seen[key] = true
			distinct = append(distinct, item)
		}
	}

	return distinct, nil
}

// groupKey is a group value's identity: its JSON encoding, so the string "1"
// and the number 1 stay apart.
func groupKey(value any) string {
	encoded, _ := json.Marshal(value)

	return string(encoded)
}
