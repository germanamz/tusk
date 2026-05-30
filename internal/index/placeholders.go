package index

import "strings"

// inPlaceholders returns the comma-separated "?,?,..." list of count bind
// markers for a SQL IN (...) clause. count must be > 0.
func inPlaceholders(count int) string {
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}
