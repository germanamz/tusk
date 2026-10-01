package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/claudeplugin"
	"github.com/germanamz/tusk/internal/contextcompose"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/status"
	"github.com/germanamz/tusk/internal/version"
	"github.com/germanamz/tusk/internal/workspace"
)

// newClaudeCmd builds the `tusk claude` group: the Claude Code plugin that
// gives every session opened in a vault a digest of it and the tusk MCP tools.
func newClaudeCmd() *cobra.Command {
	claudeCmd := &cobra.Command{
		Use:   "claude",
		Short: "Install and inspect the Claude Code plugin for this workspace",
		Long: `Manage the Claude Code plugin tusk ships for this workspace.

The plugin lives at <workspace>/.claude/skills/tusk/ and Claude Code loads it
for any session started in the workspace root. It carries two things:

  * The tusk MCP server ("tusk mcp"), so the agent has the tusk_* tools.
  * A hook that runs "tusk claude context" when a conversation starts and adds
    its output (a short orientation plus the [context] digest) to the first
    message, the way CLAUDE.md is added. /clear and compaction re-read it.

Run "tusk claude install" once per workspace (or "tusk init --claude"), and
again after upgrading tusk to refresh the plugin.`,
	}

	claudeCmd.AddCommand(newClaudeInstallCmd())
	claudeCmd.AddCommand(newClaudeUninstallCmd())
	claudeCmd.AddCommand(newClaudeStatusCmd())
	claudeCmd.AddCommand(newClaudeContextCmd())

	return claudeCmd
}

func newClaudeInstallCmd() *cobra.Command {
	var options claudeplugin.InstallOptions

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install the Claude Code plugin into this workspace",
		Long: `Write the tusk plugin to <workspace>/.claude/skills/tusk/.

Install also approves the plugin's MCP server for you: Claude Code holds a
project plugin's MCP server until each user approves it, so install adds
"plugin:tusk:tusk" to enabledMcpjsonServers in .claude/settings.local.json
(your per-user, uncommitted settings).

If .mcp.json runs its own "tusk mcp" server, install removes that entry
(and the file, when it held nothing else) so the tools are not registered
twice. Pass --keep-mcp-json to leave it.

Re-running install is safe: it rewrites the files it owns. It refuses a
.claude/skills/tusk folder it did not write unless you pass --force.

The plugin runs the binary named by --bin, "tusk" on PATH by default. A
relative path resolves against the workspace root, where sessions start.`,
		Example: `  # Install for this workspace
  tusk claude install

  # Point the plugin at a locally built binary
  tusk claude install --bin ./bin/tusk`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, rootErr := claudeWorkspaceRoot()

			if rootErr != nil {
				return rootErr
			}

			options.Root = root
			options.Version = version.Current

			return runClaudeInstall(cmd.OutOrStdout(), options)
		},
	}

	installCmd.Flags().StringVar(&options.Bin, "bin", claudeplugin.DefaultBin, "tusk binary the plugin runs")
	installCmd.Flags().BoolVar(&options.Force, "force", false, "replace a .claude/skills/tusk folder tusk did not write")
	installCmd.Flags().BoolVar(&options.KeepMCPJSON, "keep-mcp-json", false, "leave a .mcp.json tusk server in place")

	return installCmd
}

// runClaudeInstall installs the plugin and prints what changed. Shared by
// `tusk claude install` and `tusk init --claude`.
func runClaudeInstall(out io.Writer, options claudeplugin.InstallOptions) error {
	report, installErr := claudeplugin.Install(options)

	if errors.Is(installErr, claudeplugin.ErrForeignPlugin) {
		return fmt.Errorf("claude install: %s exists and tusk did not write it; pass --force to replace it", claudeplugin.PluginDir(options.Root))
	}

	if installErr != nil {
		return fmt.Errorf("claude install: %w", installErr)
	}

	if warning := claudeBinaryWarning(options.Root, options.Bin); warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}

	_, _ = fmt.Fprintf(out, "Installed the tusk plugin for Claude Code (%s).\n", options.Version)
	writeClaudeReport(out, report)
	_, _ = fmt.Fprintf(out, "\nStart Claude Code from %s: it loads the plugin from the folder a session starts in, and asks you to trust that folder the first time.\n", options.Root)

	return nil
}

func newClaudeUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the Claude Code plugin from this workspace",
		Long: `Remove <workspace>/.claude/skills/tusk/ and the plugin's MCP approval from
.claude/settings.local.json. A .mcp.json entry install migrated is not
restored; re-add it with:

  claude mcp add --scope project tusk -- tusk mcp`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, rootErr := claudeWorkspaceRoot()

			if rootErr != nil {
				return rootErr
			}

			report, uninstallErr := claudeplugin.Uninstall(root)

			if errors.Is(uninstallErr, claudeplugin.ErrForeignPlugin) {
				return fmt.Errorf("claude uninstall: %s was not written by tusk; remove it yourself", claudeplugin.PluginDir(root))
			}

			if uninstallErr != nil {
				return fmt.Errorf("claude uninstall: %w", uninstallErr)
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Removed the tusk plugin for Claude Code.")
			writeClaudeReport(cmd.OutOrStdout(), report)

			return nil
		},
	}
}

func newClaudeStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report the Claude Code plugin installed in this workspace",
		Long: `Report whether the plugin is installed, the tusk version it was installed
by against this binary's, the binary it runs and whether that resolves,
whether its MCP server is approved, and whether .mcp.json still runs a
duplicate tusk server.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, rootErr := claudeWorkspaceRoot()

			if rootErr != nil {
				return rootErr
			}

			report, statusErr := claudeplugin.Status(root)

			if statusErr != nil {
				return fmt.Errorf("claude status: %w", statusErr)
			}

			writeClaudeStatus(cmd.OutOrStdout(), root, report)

			return nil
		},
	}
}

func writeClaudeStatus(out io.Writer, root string, report *claudeplugin.StatusReport) {
	switch {
	case !report.Installed:
		_, _ = fmt.Fprintf(out, "plugin:     not installed (run tusk claude install)\n")
	case report.Foreign:
		_, _ = fmt.Fprintf(out, "plugin:     %s exists but tusk did not write it\n", report.PluginDir)
	default:
		_, _ = fmt.Fprintf(out, "plugin:     %s\n", report.PluginDir)

		current := strings.TrimPrefix(version.Current, "v")

		if report.Version == current {
			_, _ = fmt.Fprintf(out, "version:    %s (matches this binary)\n", report.Version)
		} else {
			_, _ = fmt.Fprintf(out, "version:    %s (this binary is %s; run tusk claude install)\n", report.Version, current)
		}

		binLine := report.Bin

		if warning := claudeBinaryWarning(root, report.Bin); warning != "" {
			binLine += " (" + warning + ")"
		}

		_, _ = fmt.Fprintf(out, "binary:     %s\n", binLine)
	}

	approval := "approved"

	if !report.MCPApproved {
		approval = "not approved (run tusk claude install, or approve it in Claude Code's /mcp)"
	}

	_, _ = fmt.Fprintf(out, "mcp server: %s %s\n", claudeplugin.MCPServerID, approval)

	if report.DuplicateMCPJSON {
		_, _ = fmt.Fprintln(out, ".mcp.json:  still runs its own tusk server (tusk claude install removes it)")
	}
}

func writeClaudeReport(out io.Writer, report *claudeplugin.Report) {
	for _, note := range report.Notes {
		_, _ = fmt.Fprintf(out, "  - %s\n", note)
	}

	for _, warning := range report.Warnings {
		_, _ = fmt.Fprintf(out, "warning: %s\n", warning)
	}
}

// claudeWorkspaceRoot finds the workspace the claude verbs act on.
func claudeWorkspaceRoot() (string, error) {
	cwd, cwdErr := os.Getwd()

	if cwdErr != nil {
		return "", cwdErr
	}

	ws, findErr := workspace.Find(cwd)

	if findErr != nil {
		return "", fmt.Errorf("claude: no tusk.toml in this folder or above; run tusk init first")
	}

	return ws.Root, nil
}

// claudeBinaryWarning says when the plugin's binary will not start, or will
// start a different tusk than the one installing it. A relative path resolves
// against the workspace root, the folder Claude Code sessions start in.
func claudeBinaryWarning(root, bin string) string {
	target := bin

	if strings.ContainsRune(bin, filepath.Separator) && !filepath.IsAbs(bin) {
		target = filepath.Join(root, bin)
	}

	resolved, lookErr := exec.LookPath(target)

	if lookErr != nil {
		return fmt.Sprintf("%s does not resolve to an executable; the plugin cannot start it (pass --bin with its path)", bin)
	}

	self, selfErr := os.Executable()

	if selfErr != nil {
		return ""
	}

	resolvedInfo, resolvedErr := os.Stat(resolved)
	selfInfo, selfStatErr := os.Stat(self)

	if resolvedErr != nil || selfStatErr != nil || os.SameFile(resolvedInfo, selfInfo) {
		return ""
	}

	return fmt.Sprintf("%s resolves to %s, not the binary running this command (%s); the plugin will run %s", bin, resolved, self, resolved)
}

func newClaudeContextCmd() *cobra.Command {
	var (
		pluginVersion string
		maxBytes      int
	)

	contextCmd := &cobra.Command{
		Use:   "context",
		Short: "Print the context block the Claude Code plugin adds to a session",
		Long: `Print the block the plugin adds to the first message of a Claude Code
conversation: a short orientation (the vault's declared node types with
counts, edge types, aliases, index state), then the [context] digest
("tusk context") when tusk.toml declares one.

The block stays within [context] max-bytes (default 16384). Over budget, it
drops alias sections first, then recent nodes, then pinned nodes, a whole node
at a time, and says so in its last line. The orientation is never cut.

It never fails a session: outside a workspace it prints nothing, a broken
tusk.toml prints a one-line notice, and a missing index prints the
orientation alone. It always exits 0.`,
		Example: `  # Preview what a Claude Code session sees
  tusk claude context

  # Preview with a tighter budget
  tusk claude context --max-bytes 4096`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClaudeContext(cmd, pluginVersion, maxBytes)
		},
	}

	contextCmd.Flags().IntVar(&maxBytes, "max-bytes", 0, "override the block's byte budget ([context] max-bytes, default 16384)")
	contextCmd.Flags().StringVar(&pluginVersion, "plugin-version", "", "tusk version the calling plugin was installed by; a mismatch warns on stderr")
	_ = contextCmd.Flags().MarkHidden("plugin-version")

	return contextCmd
}

func runClaudeContext(cmd *cobra.Command, pluginVersion string, maxBytes int) error {
	out := cmd.OutOrStdout()

	if pluginVersion != "" && pluginVersion != version.Current {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "tusk plugin is %s, binary is %s; run tusk claude install\n", pluginVersion, version.Current)
	}

	cwd, cwdErr := os.Getwd()

	if cwdErr != nil {
		return nil
	}

	ws, findErr := workspace.Find(cwd)

	if findErr != nil {
		return nil
	}

	loaded, loadErr := manifest.Load(ws.ManifestPath)

	if loadErr != nil {
		_, _ = fmt.Fprintf(out, "tusk.toml failed to load: %v\n", loadErr)

		return nil
	}

	// Declared types are read before the built-in sub-unit pack merges in, so
	// the orientation names what the vault's author declared.
	snap := claudeplugin.Snapshot{
		Name:      loaded.Workspace.Name,
		NodeTypes: map[string]int{},
		EdgeTypes: sortedKeys(loaded.EdgeTypes),
	}

	if snap.Name == "" {
		snap.Name = filepath.Base(ws.Root)
	}

	for name := range loaded.NodeTypes {
		snap.NodeTypes[name] = 0
	}

	manifest.MergeBuiltinPacks(loaded)

	introspect := buildVerbIntrospector(cmd.Root())
	manifest.ValidateAliases(loaded, introspect)
	manifest.ValidateContext(loaded, introspect)

	snap.Aliases = sortedKeys(loaded.Aliases)

	budget := claudeplugin.DefaultMaxBytes

	if loaded.Context != nil && loaded.Context.MaxBytes > 0 {
		budget = loaded.Context.MaxBytes
	}

	if maxBytes > 0 {
		budget = maxBytes
	}

	store, state := openIndexForClaude(ws.IndexPath)
	snap.Index = state

	var sections []claudeplugin.Section

	if store != nil {
		defer store.Close()

		if countErr := fillSnapshotCounts(&snap, store); countErr != nil {
			snap.Index = claudeplugin.IndexUnreadable
		} else {
			sections = claudeDigestSections(cmd, store, loaded, ws)
		}
	}

	orientation := claudeplugin.Orientation(snap)
	block, fitErr := claudeplugin.Fit(orientation, sections, budget)

	if fitErr != nil {
		block = orientation + fmt.Sprintf("\nThe [context] digest is unavailable: %v\n", fitErr)
	}

	_, _ = io.WriteString(out, block)

	return nil
}

// openIndexForClaude opens the index without the rebuild every other command
// falls back to: a session start must not wait on one. The MCP server builds
// or rebuilds the index when it starts.
func openIndexForClaude(indexPath string) (*index.Index, claudeplugin.IndexState) {
	if _, statErr := os.Stat(indexPath); statErr != nil {
		return nil, claudeplugin.IndexMissing
	}

	store, openErr := index.Open(indexPath)

	if openErr != nil {
		return nil, claudeplugin.IndexUnreadable
	}

	return store, claudeplugin.IndexReady
}

// fillSnapshotCounts reads node counts for the declared types, edge counts
// for the declared edge types, and the queue depths.
func fillSnapshotCounts(snap *claudeplugin.Snapshot, store *index.Index) error {
	edges := index.NewEdgeRepo(store)

	result, runErr := status.Run(status.Request{
		Nodes:      index.NewNodeRepo(store),
		Edges:      edges,
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Meta:       index.NewMetaRepo(store),
	})

	if runErr != nil {
		return runErr
	}

	for name := range snap.NodeTypes {
		snap.NodeTypes[name] = result.NodesByType[name]
	}

	for _, edgeType := range snap.EdgeTypes {
		rows, listErr := edges.ListByType(edgeType)

		if listErr != nil {
			return listErr
		}

		snap.EdgeCount += len(rows)
	}

	snap.ReindexQueue = result.ReindexQueueDepth
	snap.EmbedQueue = result.EmbedQueueDepth

	return nil
}

// claudeDigestSections composes the [context] digest and wraps each part as a
// budgeted section, highest priority first: pinned, missing pinned, recent,
// then each include alias. A digest that fails to compose becomes a one-line
// section saying so.
func claudeDigestSections(cmd *cobra.Command, store *index.Index, loaded *manifest.Manifest, ws *workspace.Workspace) []claudeplugin.Section {
	if loaded.Context == nil {
		return nil
	}

	dispatcher := aliasdispatch.NewDispatcher(newAliasDeps(store, loaded, ws, buildEmbedder(loaded)))

	result, composeErr := contextcompose.Compose(cmd.Context(), contextcompose.Deps{
		Manifest:      loaded,
		Dispatcher:    dispatcher,
		WorkspaceRoot: ws.Root,
		Database:      store.DB(),
	}, contextcompose.Request{})

	if composeErr != nil {
		return []claudeplugin.Section{claudeTextSection("Digest", fmt.Sprintf("unavailable: %v", composeErr))}
	}

	sections := []claudeplugin.Section{claudeRowsSection("Pinned", result.Pinned)}

	if len(result.MissingPinned) > 0 {
		sections = append(sections, claudeTextSection("Missing pinned", strings.Join(result.MissingPinned, "\n")))
	}

	sections = append(sections, claudeRowsSection("Recent", result.Recent))

	for _, name := range contextcompose.SortedIncludeNames(result) {
		dispatched := result.Aliases[name]

		sections = append(sections, claudeplugin.Section{
			Heading: "Aliases / " + name,
			Items:   1,
			Whole:   true,
			Render: func(int) (string, error) {
				var builder strings.Builder

				renderErr := renderAliasResult(&builder, dispatched, formatCompact)

				return builder.String(), renderErr
			},
		})
	}

	return sections
}

func claudeRowsSection(heading string, rows []query.ListRow) claudeplugin.Section {
	return claudeplugin.Section{
		Heading: heading,
		Items:   len(rows),
		Render: func(count int) (string, error) {
			var builder strings.Builder

			renderErr := writeCompactNodeRowsBuilder(&builder, rows[:count])

			return builder.String(), renderErr
		},
	}
}

func claudeTextSection(heading, text string) claudeplugin.Section {
	return claudeplugin.Section{
		Heading: heading,
		Items:   1,
		Whole:   true,
		Render: func(int) (string, error) {
			return text, nil
		},
	}
}

func sortedKeys[Value any](entries map[string]Value) []string {
	keys := make([]string, 0, len(entries))

	for key := range entries {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
