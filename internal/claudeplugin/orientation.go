package claudeplugin

import (
	"fmt"
	"sort"
	"strings"
)

// IndexState says what the orientation can claim about the workspace index.
type IndexState int

const (
	// IndexReady means the index opened and its counts are real.
	IndexReady IndexState = iota
	// IndexMissing means no index file exists yet (a fresh clone).
	IndexMissing
	// IndexUnreadable means the index exists but did not open (an older
	// schema, a corrupt file); the MCP server rebuilds it on start.
	IndexUnreadable
)

// Snapshot is what the orientation describes. NodeTypes holds the node types
// tusk.toml declares (not the built-in sub-unit types) with their indexed
// counts; counts are ignored unless Index is IndexReady.
type Snapshot struct {
	Name         string
	NodeTypes    map[string]int
	EdgeTypes    []string
	EdgeCount    int
	Aliases      []string
	Index        IndexState
	ReindexQueue int
	EmbedQueue   int
}

// Orientation renders the always-present head of the context block: what
// the vault is, what it holds and whether its index is current. The MCP
// server's own instructions already teach the tools, so this stays short.
func Orientation(snap Snapshot) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "This project is a tusk vault (%q): its markdown files are indexed as a typed graph.\n", snap.Name)
	builder.WriteString("Use the tusk_* tools to query it before grepping files.\n")
	builder.WriteString("Node types: " + nodeTypesLine(snap) + "\n")

	if line := edgesLine(snap); line != "" {
		builder.WriteString(line + "\n")
	}

	if len(snap.Aliases) > 0 {
		builder.WriteString("Aliases: " + strings.Join(sortedCopy(snap.Aliases), ", ") + " (run with tusk_run)\n")
	}

	builder.WriteString("Index: " + indexLine(snap) + "\n")

	return builder.String()
}

func nodeTypesLine(snap Snapshot) string {
	if len(snap.NodeTypes) == 0 {
		return "none declared in tusk.toml"
	}

	names := make([]string, 0, len(snap.NodeTypes))

	for name := range snap.NodeTypes {
		names = append(names, name)
	}

	if snap.Index != IndexReady {
		return strings.Join(sortedCopy(names), " · ")
	}

	sort.Slice(names, func(left, right int) bool {
		leftCount, rightCount := snap.NodeTypes[names[left]], snap.NodeTypes[names[right]]

		if leftCount != rightCount {
			return leftCount > rightCount
		}

		return names[left] < names[right]
	})

	parts := make([]string, 0, len(names))

	for _, name := range names {
		if count := snap.NodeTypes[name]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", name, count))
		}
	}

	if len(parts) == 0 {
		return "none indexed yet (declared: " + strings.Join(sortedCopy(names), ", ") + ")"
	}

	return strings.Join(parts, " · ")
}

func edgesLine(snap Snapshot) string {
	types := strings.Join(sortedCopy(snap.EdgeTypes), ", ")

	if snap.Index != IndexReady {
		if types == "" {
			return ""
		}

		return "Edge types: " + types
	}

	if types == "" {
		return fmt.Sprintf("Edges: %d", snap.EdgeCount)
	}

	return fmt.Sprintf("Edges: %d · %s", snap.EdgeCount, types)
}

func indexLine(snap Snapshot) string {
	switch snap.Index {
	case IndexMissing:
		return "not built yet; the tusk MCP server builds it on start"
	case IndexUnreadable:
		return "needs a rebuild; the tusk MCP server rebuilds it on start"
	}

	var parts []string

	if snap.ReindexQueue > 0 {
		parts = append(parts, fmt.Sprintf("%d files queued for reindex", snap.ReindexQueue))
	}

	if snap.EmbedQueue > 0 {
		parts = append(parts, fmt.Sprintf("%d embeddings pending", snap.EmbedQueue))
	}

	if len(parts) == 0 {
		return "up to date"
	}

	return strings.Join(parts, " · ")
}

func sortedCopy(values []string) []string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)

	return sorted
}
