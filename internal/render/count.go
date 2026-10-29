package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/query"
)

// noGroupValue labels the group of matches with no group-by value.
const noGroupValue = "(none)"

// Counts writes a count query's result as text: the total alone, or with a
// group-by a <KEY> / COUNT table in the result's group order.
func Counts(out io.Writer, result *query.CountResult) error {
	if result.GroupBy == "" {
		_, writeErr := fmt.Fprintln(out, result.Total)

		return writeErr
	}

	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintf(tab, "%s\tCOUNT\n", strings.ToUpper(result.GroupBy))

	for _, group := range result.Groups {
		_, _ = fmt.Fprintf(tab, "%s\t%d\n", groupLabel(group.Value), group.Count)
	}

	return tab.Flush()
}

// groupLabel is a group value as the table shows it: a string as itself, no
// value as (none), and anything else (numbers, booleans) in its JSON form.
func groupLabel(value any) string {
	switch typed := value.(type) {
	case nil:
		return noGroupValue
	case string:
		return typed
	}

	encoded, _ := json.Marshal(value)

	return string(encoded)
}
