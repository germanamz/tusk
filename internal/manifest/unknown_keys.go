package manifest

import (
	"sort"

	"github.com/BurntSushi/toml"
)

// UnknownKey is a key in tusk.toml that no part of tusk reads.
type UnknownKey struct {
	// Key is the dotted path as tusk.toml spells it: workspace.nmae, or
	// edge-types.depends-on.acylic.
	Key string

	// Table is true when the key names a table, such as [mystery].
	Table bool
}

// recordUnknownKeys stamps loaded.UnknownKeys with every key that no decode
// pass read. Each pass decodes part of the file and leaves the rest
// undecoded, so a key is unknown when every pass left it undecoded. Only the
// outermost key of an unknown table is listed, and a top-level table no pass
// read any part of is listed alone, even when only its sub-tables are written.
// Skipped: keys inside a [rule.<name>] or [check.<name>] block, which
// rule-invalid and check-invalid report with the block's own message, and a
// behavior instance's settings, which its behavior reads.
func recordUnknownKeys(loaded *Manifest) {
	if len(loaded.decodeMetas) == 0 {
		return
	}

	undecodedBy := map[string]int{}
	keys := map[string]toml.Key{}

	for _, meta := range loaded.decodeMetas {
		for _, key := range meta.Undecoded() {
			name := key.String()
			undecodedBy[name]++
			keys[name] = key
		}
	}

	// A top-level name some pass read part of. `[mystery.lint]` alone leaves
	// mystery implicit, so no pass lists it; its keys collapse onto it.
	readTopLevel := map[string]struct{}{}

	for _, meta := range loaded.decodeMetas {
		undecoded := map[string]struct{}{}

		for _, key := range meta.Undecoded() {
			undecoded[key.String()] = struct{}{}
		}

		for _, key := range meta.Keys() {
			if _, skipped := undecoded[key.String()]; !skipped {
				readTopLevel[key[0]] = struct{}{}
			}
		}
	}

	unknown := map[string]toml.Key{}

	// collapsed holds the top-level names that stand in for their keys; each
	// names a table, implicit or not.
	collapsed := map[string]struct{}{}

	for name, count := range undecodedBy {
		key := keys[name]

		if count != len(loaded.decodeMetas) || keyOwnedElsewhere(key) {
			continue
		}

		if _, read := readTopLevel[key[0]]; !read && len(key) > 1 {
			key = key[:1]
			collapsed[key.String()] = struct{}{}
		}

		unknown[key.String()] = key
	}

	loaded.UnknownKeys = nil

	for name, key := range unknown {
		if hasUnknownAncestor(key, unknown) {
			continue
		}

		_, isCollapsed := collapsed[name]

		loaded.UnknownKeys = append(loaded.UnknownKeys, UnknownKey{
			Key:   name,
			Table: isCollapsed || loaded.decodeMetas[0].Type(key...) == "Hash",
		})
	}

	sort.Slice(loaded.UnknownKeys, func(left, right int) bool {
		return loaded.UnknownKeys[left].Key < loaded.UnknownKeys[right].Key
	})
}

// keyOwnedElsewhere reports whether another check reports the key: a key in
// a [rule.<name>] or [check.<name>] block, or a behavior instance's setting.
func keyOwnedElsewhere(key toml.Key) bool {
	switch {
	case len(key) >= 2 && (key[0] == "rule" || key[0] == "check"):
		return true
	case len(key) >= 4 && key[0] == "behaviors":
		return true
	}

	return false
}

func hasUnknownAncestor(key toml.Key, unknown map[string]toml.Key) bool {
	for length := 1; length < len(key); length++ {
		if _, isUnknown := unknown[key[:length].String()]; isUnknown {
			return true
		}
	}

	return false
}
