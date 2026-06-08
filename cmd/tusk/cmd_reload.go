package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/manifestepoch"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newReloadCmd() *cobra.Command {
	reloadCmd := &cobra.Command{
		Use:   "reload",
		Short: "Hot-reload the manifest (tusk.toml) without restarting the daemon",
		Long: `Validate and reload the manifest (tusk.toml) while any running daemon
processes converge to the new schema. Unlike "tusk reset" (which drops the
index), reload re-reads and validates tusk.toml in place, atomically swaps
the in-memory schema, and optionally triggers a reindex to re-validate
already-indexed content against the new schema.

A reload is non-interactive and non-destructive: the file watcher continues
to ignore tusk.toml (reload is explicit-only), and all running daemons
converge via the manifest-epoch sentinel (no need to restart).

By default no local reindex runs — a running daemon converges via the
manifest-epoch sentinel and owns the reindex. Pass --reindex to run a
synchronous reindex pass in this process (for the no-daemon scenario).

Validation matches boot semantics: a TOML parse/structural error or
behavior-engine build failure aborts the reload (exit non-zero, no epoch
bump); dangling aliases and invalid [context] entries are dropped and
reported as warnings while the swap still proceeds.`,
		Example: `  # Reload the manifest; daemon handles reindex if running
  tusk reload

  # Reload and synchronously reindex (for no-daemon scenarios)
  tusk reload --reindex`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			// newLogger always returns a non-nil *slog.Logger, so no nil guards
			// are needed at the call sites below.
			logger := newLogger(cmd.ErrOrStderr(), verbose)

			// Load the manifest off-lock to avoid blocking daemon readers
			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return fmt.Errorf("manifest: %w", loadErr)
			}

			// Merge built-in packs (same as boot path)
			manifest.MergeBuiltinPacks(loaded)

			// Validate with lenient alias/context gates (match boot semantics).
			// These populate AliasErrors and ContextErrors but do not abort.
			// Build the verb introspector from the command tree so alias/context
			// validation matches boot semantics (same as the daemon path).
			introspector := buildVerbIntrospector(cmd.Root())

			logger.Debug("validating aliases")
			manifest.ValidateAliases(loaded, introspector)

			logger.Debug("validating context")
			manifest.ValidateContext(loaded, introspector)

			// Gate on behavior engine build (blocking failure)
			engine, buildErr := newBehaviorEngine(loaded)

			if buildErr != nil {
				return fmt.Errorf("behavior engine: %w", buildErr)
			}

			logger.Debug("manifest validation succeeded")

			// All blocking validation passed; bump the epoch
			newEpoch, bumpErr := manifestepoch.Bump(ws.Root)

			if bumpErr != nil {
				return fmt.Errorf("manifestepoch: %w", bumpErr)
			}

			logger.Info("manifest epoch bumped", "epoch", newEpoch)

			// A fresh CLI process has no previously-loaded manifest, so it cannot
			// compute a true added/removed diff (that is the daemon tool's job).
			// Instead report the LOADED SCHEMA SUMMARY: the counts and names of
			// node-types, edge-types, and behaviors now in effect, plus the new
			// epoch. (DiffManifests is unused here precisely because there is no
			// previous manifest to diff against.)
			nodeTypeNames := make([]string, 0, len(loaded.NodeTypes))
			for nodeTypeName := range loaded.NodeTypes {
				nodeTypeNames = append(nodeTypeNames, nodeTypeName)
			}

			edgeTypeNames := make([]string, 0, len(loaded.EdgeTypes))
			for edgeTypeName := range loaded.EdgeTypes {
				edgeTypeNames = append(edgeTypeNames, edgeTypeName)
			}

			behaviorRefs := make([]map[string]string, 0)
			for behaviorKind, instances := range loaded.Behaviors {
				for instanceName := range instances {
					behaviorRefs = append(behaviorRefs, map[string]string{
						"kind":     behaviorKind,
						"instance": instanceName,
					})
				}
			}

			// Construct the response envelope (schema summary, not a diff)
			response := map[string]interface{}{
				"manifest_epoch": newEpoch,
				"schema": map[string]interface{}{
					"node_types": map[string]interface{}{
						"count": len(nodeTypeNames),
						"names": nodeTypeNames,
					},
					"edge_types": map[string]interface{}{
						"count": len(edgeTypeNames),
						"names": edgeTypeNames,
					},
					"behaviors": map[string]interface{}{
						"count": len(behaviorRefs),
						"items": behaviorRefs,
					},
				},
				"validation_errors": []string{},
				"warnings":          []string{},
			}

			// Collect alias/context warnings
			var warnings []string
			for _, aliasErr := range loaded.AliasErrors {
				warnings = append(warnings, fmt.Sprintf("alias %q: %s", aliasErr.Name, aliasErr.Message))
			}
			for _, contextErr := range loaded.ContextErrors {
				warnings = append(warnings, fmt.Sprintf("context: %s", contextErr.Message))
			}

			if len(warnings) > 0 {
				response["warnings"] = warnings
			}

			// Marshal and print the response
			jsonOut, jsonErr := json.MarshalIndent(response, "", "  ")

			if jsonErr != nil {
				return fmt.Errorf("json: %w", jsonErr)
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(jsonOut))

			// Optional reindex (off-lock, async false for CLI = synchronous).
			// Default is no local reindex — a running daemon converges and
			// reindexes; only --reindex forces a synchronous local pass.
			reindexFlag, _ := cmd.Flags().GetBool("reindex")

			if reindexFlag {
				logger.Info("reindexing (synchronous)")

				store, openErr := index.Open(ws.IndexPath)

				if openErr != nil {
					logger.Warn("index open for reindex", "err", openErr)
					// Continue; index may not exist yet
					return nil
				}

				defer func() { _ = store.Close() }()

				reindexWorkers := resolveEmbedWorkers(loaded)

				cfg := reindex.Config{
					Root:            ws.Root,
					Repo:            index.NewNodeRepo(store),
					Edges:           index.NewEdgeRepo(store),
					EdgeTypes:       loaded.EdgeTypes,
					WorkspaceIgnore: loaded.Workspace.Ignore,
					EmbedQueue:      index.NewEmbedQueueRepo(store),
					Meta:            index.NewMetaRepo(store),
					FileStates:      index.NewFileStateRepo(store),
					Behaviors:       engine,
					DriftLog:        index.NewWorkflowDriftRepo(store),
					NodeTypes:       loaded.NodeTypes,
					PropertyDrift:   index.NewPropertyDriftRepo(store),
					Logger:          logger,
					Workers:         reindexWorkers,
					Manifest:        loaded,
					Async:           false, // CLI blocks until reindex completes
				}

				if embedder := buildEmbedder(loaded); embedder != nil {
					cfg.Embedder = embedder
					cfg.Chunker = embed.MarkdownRecursive{}
					cfg.EmbeddingRepo = index.NewEmbeddingRepo(store)
				}

				report, runErr := reindex.Run(cfg)

				if runErr != nil {
					logger.Warn("reindex failed", "err", runErr)
					// Log but do not fail the whole reload; epoch was already bumped
					return nil
				}

				logger.Info("reindex completed",
					"indexed", report.Indexed,
					"removed", report.Removed,
					"skipped", report.Skipped)
			}

			return nil
		},
	}

	reloadCmd.Flags().BoolP("verbose", "v", false, "emit debug-level logs to stderr")
	reloadCmd.Flags().Bool("reindex", false, "synchronously reindex after reloading the manifest")

	return reloadCmd
}
