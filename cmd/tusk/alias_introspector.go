package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/germanamz/tusk/internal/manifest"
)

// buildVerbIntrospector walks rootCmd and returns a VerbIntrospector that
// reports the (local + persistent) flag set of each verb. The verb name is
// the space-joined sub-command path (e.g. "node list", "edge list",
// "doctor"). Used by manifest.ValidateAliases to type-check alias arg keys
// against the live Cobra command tree.
func buildVerbIntrospector(rootCmd *cobra.Command) manifest.VerbIntrospector {
	flagsByVerb := collectVerbFlags(rootCmd)

	return func(verb string) ([]manifest.FlagSpec, bool) {
		flags, ok := flagsByVerb[verb]

		return flags, ok
	}
}

// collectVerbFlags walks rootCmd recursively and returns a map keyed on the
// space-joined sub-command path. Only leaf commands (no further
// sub-commands) get an entry, since the alias mechanism targets verbs that
// actually run, not parent groups.
func collectVerbFlags(rootCmd *cobra.Command) map[string][]manifest.FlagSpec {
	out := map[string][]manifest.FlagSpec{}
	walkLeaves(rootCmd, nil, out)

	return out
}

func walkLeaves(node *cobra.Command, parents []string, out map[string][]manifest.FlagSpec) {
	if !node.HasSubCommands() {
		// Skip the root itself: parents is empty when node==root.
		if len(parents) == 0 {
			return
		}

		verb := strings.Join(parents, " ")
		out[verb] = collectFlags(node)

		return
	}

	for _, child := range node.Commands() {
		childPath := append(append([]string{}, parents...), child.Name())
		walkLeaves(child, childPath, out)
	}
}

// collectFlags merges local and inherited flag sets into a FlagSpec slice.
// Persistent root flags (e.g. --verbose) are included so an alias can set
// them.
func collectFlags(cmd *cobra.Command) []manifest.FlagSpec {
	seen := map[string]struct{}{}

	var out []manifest.FlagSpec

	visit := func(flag *pflag.Flag) {
		if _, dup := seen[flag.Name]; dup {
			return
		}

		seen[flag.Name] = struct{}{}
		out = append(out, manifest.FlagSpec{Name: flag.Name, Kind: pflagKind(flag)})
	}

	cmd.Flags().VisitAll(visit)
	cmd.InheritedFlags().VisitAll(visit)

	return out
}

// pflagKind classifies a pflag.Flag into the kind strings ValidateAliases
// understands. Anything not in the supported set falls back to "string" so
// the alias loader treats it permissively (the dispatcher will fail loudly
// if the value is wrong at run time).
func pflagKind(flag *pflag.Flag) string {
	switch flag.Value.Type() {
	case "string":
		return "string"
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "count":
		return "int"
	case "bool":
		return "bool"
	case "stringSlice", "stringArray":
		return "stringSlice"
	}

	return "string"
}
