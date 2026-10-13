// Package packs compiles the built-in type packs into the binary, so `tusk
// pack add <name>` and `tusk init` apply them without a network fetch and
// always at the version the binary understands. The TOML files stay at
// packs/*.toml because tusk releases before #790 fetch them from there.
package packs

import (
	"embed"
	"slices"
	"strings"
)

//go:embed kanban.toml tags.toml vault.toml
var builtin embed.FS

// Names returns the built-in pack names, sorted.
func Names() []string {
	entries, _ := builtin.ReadDir(".")
	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		names = append(names, strings.TrimSuffix(entry.Name(), ".toml"))
	}

	slices.Sort(names)

	return names
}

// Read returns the named built-in pack's TOML, and false when no built-in
// pack has that name.
func Read(name string) ([]byte, bool) {
	if !slices.Contains(Names(), name) {
		return nil, false
	}

	body, readErr := builtin.ReadFile(name + ".toml")

	return body, readErr == nil
}
