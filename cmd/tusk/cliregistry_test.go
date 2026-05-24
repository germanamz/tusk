package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/cliregistry"
	"github.com/germanamz/tusk/internal/mcp"
)

// TestRegistry_NoOrphanCobraCommands walks the live Cobra command tree and
// asserts every leaf sub-command path the CLI exposes appears in either
// cliregistry.ReadOnly or cliregistry.Write. Catches the case where a new
// verb is added to Cobra but not threaded through the registry — which would
// silently exclude it from the alias dispatcher.
func TestRegistry_NoOrphanCobraCommands(test *testing.T) {
	rootCmd := newRootCmd()

	// Skip the synthetic root and Cobra's auto-generated commands. The
	// registry only cares about verbs the user can invoke as workspace
	// operations. Per-workspace verbs (init, reindex, watch, mcp, pack add,
	// docgen) are not part of the read/write split the alias dispatcher
	// operates over.
	excluded := map[string]struct{}{
		"help":       {},
		"completion": {},
		// Workspace lifecycle / non-aliased operations (out of scope for
		// the alias dispatcher — they manage the workspace itself).
		"init":     {},
		"reindex":  {},
		"watch":    {},
		"mcp":      {},
		"pack add": {},
		"docgen":   {},
		// `run` is the dispatcher entry point itself; it invokes other
		// verbs by name rather than being one.
		"run": {},
		// `context` is the warm-context composer entry point; it fans
		// out over other aliased verbs rather than being one.
		"context": {},
	}

	leaves := collectLeafVerbs(rootCmd, "")

	for _, verb := range leaves {
		if _, skip := excluded[verb]; skip {
			continue
		}

		_, inReadOnly := cliregistry.ReadOnly[verb]
		_, inWrite := cliregistry.Write[verb]

		if !inReadOnly && !inWrite {
			test.Errorf("Cobra verb %q is missing from both cliregistry.ReadOnly and cliregistry.Write", verb)
		}
	}
}

// TestRegistry_ToolNamesMatchMCPRegistrations boots a real *mcp.Server and
// asserts every cliregistry.ReadOnly[*].Tool name matches a registered MCP
// tool. A renamed MCP tool or a stale registry entry would fail this test.
func TestRegistry_ToolNamesMatchMCPRegistrations(test *testing.T) {
	tmpDir := test.TempDir()

	manifestBody := []byte("[workspace]\nname = \"test\"\n")

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "tusk.toml"), manifestBody, 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	runtime, openErr := mcp.Open(tmpDir)

	if openErr != nil {
		test.Fatalf("mcp.Open: %v", openErr)
	}

	defer runtime.Close()

	srv := mcp.NewServer(runtime)
	registered := map[string]struct{}{}

	for _, name := range srv.RegisteredToolNames() {
		registered[name] = struct{}{}
	}

	for verb, spec := range cliregistry.ReadOnly {
		if _, found := registered[spec.Tool]; !found {
			test.Errorf("cliregistry.ReadOnly[%q].Tool = %q is not a registered MCP tool", verb, spec.Tool)
		}
	}
}

// collectLeafVerbs recurses over the Cobra tree and returns the
// space-joined path of every leaf sub-command (a "leaf" being a command
// with no further sub-commands besides Cobra's auto-generated help). The
// root's name is omitted from the returned paths so callers can match
// directly against cliregistry keys (e.g. "node list" rather than
// "tusk node list").
func collectLeafVerbs(cmd *cobra.Command, prefix string) []string {
	var verbs []string

	subs := userSubCommands(cmd)

	if len(subs) == 0 {
		if prefix == "" {
			return nil
		}

		return []string{prefix}
	}

	for _, sub := range subs {
		nextPrefix := sub.Name()

		if prefix != "" {
			nextPrefix = prefix + " " + sub.Name()
		}

		verbs = append(verbs, collectLeafVerbs(sub, nextPrefix)...)
	}

	sort.Strings(verbs)

	return verbs
}

// userSubCommands returns the user-facing sub-commands of cmd, filtering out
// Cobra auto-generated commands like `help` and `completion`.
func userSubCommands(cmd *cobra.Command) []*cobra.Command {
	var subs []*cobra.Command

	for _, sub := range cmd.Commands() {
		name := sub.Name()

		if strings.HasPrefix(name, "help") || name == "completion" {
			continue
		}

		subs = append(subs, sub)
	}

	return subs
}
