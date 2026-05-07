// Package typepacks implements `tusk pack add`: fetch, validate, and
// merge community-shared TOML pack content into the workspace manifest.
package typepacks

import (
	"fmt"
	"sort"
	"strings"
)

// BuiltinAliases maps a built-in pack short name to its canonical URL.
// The pack TOML files at these URLs ship in Plans 7.c.2/7.c.3/7.c.4;
// they 404 until then, but the alias map is committed in 7.c.1 so the
// CLI surface is complete.
var BuiltinAliases = map[string]string{
	"kanban": "https://raw.githubusercontent.com/germanamz/tusk/main/packs/kanban.toml",
	"vault":  "https://raw.githubusercontent.com/germanamz/tusk/main/packs/vault.toml",
	"tags":   "https://raw.githubusercontent.com/germanamz/tusk/main/packs/tags.toml",
}

// Resolve maps arg to a fetchable URL. If arg contains "://" it is
// treated as a URL and returned verbatim. Otherwise it must be a
// built-in name from BuiltinAliases.
func Resolve(arg string) (string, error) {
	if strings.Contains(arg, "://") {
		return arg, nil
	}

	url, found := BuiltinAliases[arg]

	if found {
		return url, nil
	}

	return "", fmt.Errorf("pack add: unknown pack name %q; supported names: %s — to install from a URL, pass the full URL", arg, supportedNames())
}

func supportedNames() string {
	names := make([]string, 0, len(BuiltinAliases))

	for name := range BuiltinAliases {
		names = append(names, name)
	}

	sort.Strings(names)

	return strings.Join(names, ", ")
}
