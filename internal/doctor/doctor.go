// Package doctor runs read-only health checks against the index and
// optionally migrates legacy edge rows back into source frontmatter.
package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// Issue kinds.
const (
	IssueDanglingEdge      = "dangling-edge"
	IssueEmbedRetry        = "embed-retry"
	IssueWorkflowViolation = "workflow-violation"

	IssueUndeclaredProperty = "undeclared-property"
	IssueTypeMismatch       = "type-mismatch"
	IssueRequiredMissing    = "required-missing"
	IssueEnumViolation      = "enum-violation"

	IssueRefDangling     = "ref_dangling"
	IssueRefAmbiguous    = "ref_ambiguous"
	IssueRefTypeMismatch = "ref_type_mismatch"
	IssueRefCycle        = "ref_cycle"

	IssueEmbedLargeChunk = "embed-large-chunk"
	IssueEmbedNoChunks   = "embed-no-chunks"

	IssueLegacyCLIEdge = "legacy-cli-edge"
	IssueLegacyMCPEdge = "legacy-mcp-edge"

	// IssueAliasInvalid surfaces manifest aliases that failed validation
	// at load time. The Manifest field on doctor.Config carries the
	// pre-validated list; Run copies them into Report.AliasErrors and
	// also emits one Issue per error so legacy renderers keep working.
	IssueAliasInvalid = "alias-invalid"

	// IssueContextInvalid surfaces manifest [context] block problems
	// (unknown alias names, both recent forms set, write-verb inline
	// recent). One Issue per ContextError so the legacy line-oriented
	// renderer keeps working alongside the typed Report.ContextErrors.
	IssueContextInvalid = "context-invalid"

	// IssueContextPinnedMissing surfaces manifest [context.pinned] IDs
	// that do not resolve in the current index. Computed at doctor-run
	// time (the IDs depend on runtime state, not manifest shape).
	IssueContextPinnedMissing = "context-pinned-missing"

	// IssueSubUnitReserved surfaces reserved-name conflicts between the
	// built-in sub-document pack and user-declared node types, edge
	// types, or properties. The engine prefers the built-in declaration
	// and ignores the user's override; doctor surfaces the override so
	// users notice the shadowing.
	IssueSubUnitReserved = "sub-unit-reserved"

	// IssueSubUnitsDisabledDirty surfaces the back-compat hazard where
	// the manifest opts out of sub-units (`[workspace] sub-units =
	// false`) but the index still contains sub-unit rows from a previous
	// run with sub-units enabled. Doctor does NOT auto-clean; the user
	// must run `tusk reindex --force` to drop the stale rows.
	IssueSubUnitsDisabledDirty = "sub-units-disabled-dirty"

	// IssueGraphExpansionUnknownEdge surfaces an entry in
	// [query.graph-expansion] edge-types that does not resolve to a
	// declared edge type in the merged manifest. The walker silently
	// skips such names; doctor flags them so users notice drift.
	IssueGraphExpansionUnknownEdge = "graph-expansion-unknown-edge"

	// IssueGraphExpansionWeightZero surfaces the no-op configuration
	// where [query.graph-expansion] enabled=true but weight=0. The
	// feature is on but contributes nothing to the blended score —
	// almost certainly a config bug.
	IssueGraphExpansionWeightZero = "graph-expansion-weight-zero"
)

// Issue is a single problem the doctor surfaced.
type Issue struct {
	Kind    string
	NodeID  string
	Message string
}

// Report is the doctor's verdict.
type Report struct {
	Issues            []Issue
	EmbedQueueDepth   int // pending rows with kind='embed'
	ReindexQueueDepth int // pending rows with kind='reindex'
	EmbedStats        *EmbedStatsReport
	// AliasErrors mirrors Manifest.AliasErrors for callers that want the
	// typed list (CLI, MCP) instead of parsing them back out of Issues.
	AliasErrors []manifest.AliasError
	// ContextErrors mirrors Manifest.ContextErrors so CLI and MCP can
	// surface invalid [context] declarations without re-parsing Issues.
	ContextErrors []manifest.ContextError
	// MissingPinnedIDs lists [context.pinned] entries that do not
	// resolve to a node in the current index. Computed at Run time.
	MissingPinnedIDs []string
	// SubUnitConflicts mirrors Manifest.SubUnitConflicts so CLI and MCP
	// can surface sub-document reserved-name overrides without
	// re-parsing Issues.
	SubUnitConflicts []manifest.SubUnitConflict
	// SubUnitPane is the typed sub-unit health summary (Plan 2 Task 6
	// / spec §5.9). nil when the manifest opts out of sub-units AND no
	// sub-unit rows exist in the index. When sub-units are disabled but
	// stale rows remain, the pane is still populated so the CLI/MCP
	// renderer can show the dirty-state counts alongside the
	// IssueSubUnitsDisabledDirty warning.
	SubUnitPane *SubUnitPane
	// GraphExpansion is the typed [query.graph-expansion] pane (Phase 3
	// Task 4). Populated whenever Config.Manifest is non-nil so the
	// renderer can surface the active values regardless of the enabled
	// flag. nil when no manifest was supplied to Run.
	GraphExpansion *GraphExpansionPane
}

// SubUnitPane summarizes the sub-unit pipeline's health for tusk doctor
// (spec §5.9). All counts are computed from the index at Run time.
type SubUnitPane struct {
	// Total is the total number of sub-unit rows
	// (`nodes.kind = 'subunit'`).
	Total int
	// CountByKind buckets the totals by `nodes.type` for sub-unit rows.
	// Keys are the subunit.Kind string values: "section", "paragraph",
	// "list-item", "code-block", "blockquote", "table-cell".
	CountByKind map[string]int
	// DedupedSubUnits counts content_hash values shared by two or more
	// sub-unit rows — groups of duplicate-content leaves that the
	// content-addressed embedding store collapses to a single shared
	// vector. Informational: a high count just means repeated content.
	DedupedSubUnits int
	// OrphanedSubUnits counts rows whose parent_id does not resolve to
	// a node row. Should always be zero (FK CASCADE), surfaced only
	// when > 0 as a "this indicates a bug" warning.
	OrphanedSubUnits int
	// EmbedQueueFiles is the number of pending file-level rows in the
	// embed queue (queued id contains no `#`).
	EmbedQueueFiles int
	// EmbedQueueSubUnits is the number of pending sub-unit rows in the
	// embed queue (queued id contains `#`).
	EmbedQueueSubUnits int
	// OversizeEmbedPayloads is the number of sub-unit rows whose
	// embed_payload byte length exceeds embed.DefaultMaxBytes. The
	// chunker normally keeps payloads under this bound; a non-zero
	// count indicates the AST emitted a single leaf exceeding the cap.
	OversizeEmbedPayloads int
	// ReservedNameConflicts mirrors len(Report.SubUnitConflicts) so the
	// pane can show the recap count without the caller re-counting.
	ReservedNameConflicts int
}

// EmbedStatsReport summarizes chunking aggregates for tusk doctor.
type EmbedStatsReport struct {
	TotalNodes   int
	TotalChunks  int
	MeanChunks   float64
	MedianChunks int
	MaxChunks    int
	TopByChunks  []index.NodeChunkCount
}

// Config configures Run.
type Config struct {
	Nodes         *index.NodeRepo
	Edges         *index.EdgeRepo
	EmbedQueue    *index.EmbedQueueRepo
	WorkflowDrift *index.WorkflowDriftRepo // optional; nil = no workflow checks
	PropertyDrift *index.PropertyDriftRepo // optional; nil = no property checks
	Embeddings    *index.EmbeddingRepo
	Manifest      *manifest.Manifest
	Root          string // workspace root; required for Migrate
}

// MigrationReport summarizes a Migrate call.
type MigrationReport struct {
	Migrated []string // human-readable lines, one per migrated edge row
	Skipped  []string // human-readable lines, one per skipped legacy row
}

// Run executes every check and returns the aggregate Report.
func Run(config Config) (*Report, error) {
	report := &Report{}

	if config.Manifest != nil && len(config.Manifest.AliasErrors) > 0 {
		report.AliasErrors = append(report.AliasErrors, config.Manifest.AliasErrors...)

		for _, aliasErr := range config.Manifest.AliasErrors {
			report.Issues = append(report.Issues, Issue{
				Kind:    IssueAliasInvalid,
				NodeID:  aliasErr.Name,
				Message: aliasErr.Message,
			})
		}
	}

	if config.Manifest != nil && len(config.Manifest.ContextErrors) > 0 {
		report.ContextErrors = append(report.ContextErrors, config.Manifest.ContextErrors...)

		for _, contextErr := range config.Manifest.ContextErrors {
			report.Issues = append(report.Issues, Issue{
				Kind:    IssueContextInvalid,
				Message: contextErr.Message,
			})
		}
	}

	if config.Manifest != nil {
		pane, issues := computeGraphExpansionPane(config.Manifest)

		report.GraphExpansion = pane
		report.Issues = append(report.Issues, issues...)
	}

	if config.Manifest != nil && len(config.Manifest.SubUnitConflicts) > 0 {
		report.SubUnitConflicts = append(report.SubUnitConflicts, config.Manifest.SubUnitConflicts...)

		for _, conflict := range config.Manifest.SubUnitConflicts {
			nodeID := conflict.Name

			if conflict.OwnerType != "" {
				nodeID = conflict.OwnerType + "." + conflict.Name
			}

			report.Issues = append(report.Issues, Issue{
				Kind:    IssueSubUnitReserved,
				NodeID:  nodeID,
				Message: conflict.Message,
			})
		}
	}

	if config.Manifest != nil && config.Nodes != nil && config.Manifest.Context != nil {
		missing := CheckPinnedNodes(config.Manifest, config.Nodes)

		if len(missing) > 0 {
			report.MissingPinnedIDs = missing

			for _, id := range missing {
				report.Issues = append(report.Issues, Issue{
					Kind:    IssueContextPinnedMissing,
					NodeID:  id,
					Message: fmt.Sprintf("context: pinned node %q does not resolve in the index", id),
				})
			}
		}
	}

	if config.Edges != nil && config.Nodes != nil {
		dangling, danglingErr := findDanglingEdges(config.Nodes, config.Edges)

		if danglingErr != nil {
			return nil, danglingErr
		}

		report.Issues = append(report.Issues, dangling...)
	}

	if config.EmbedQueue != nil {
		embedDepth, embedDepthErr := config.EmbedQueue.DepthByKind("embed")

		if embedDepthErr != nil {
			return nil, embedDepthErr
		}

		report.EmbedQueueDepth = embedDepth

		reindexDepth, reindexDepthErr := config.EmbedQueue.DepthByKind("reindex")

		if reindexDepthErr != nil {
			return nil, reindexDepthErr
		}

		report.ReindexQueueDepth = reindexDepth
	}

	if config.WorkflowDrift != nil {
		drift, listErr := config.WorkflowDrift.ListAll()

		if listErr != nil {
			return nil, listErr
		}

		for _, row := range drift {
			report.Issues = append(report.Issues, Issue{
				Kind:    IssueWorkflowViolation,
				NodeID:  row.NodeID,
				Message: renderWorkflowDriftMessage(row),
			})
		}
	}

	if config.PropertyDrift != nil {
		propDrift, listErr := config.PropertyDrift.ListAll()

		if listErr != nil {
			return nil, listErr
		}

		for _, row := range propDrift {
			report.Issues = append(report.Issues, Issue{
				Kind:    row.Kind,
				NodeID:  row.NodeID,
				Message: renderPropertyDriftMessage(row),
			})
		}
	}

	if config.Embeddings != nil && config.Manifest != nil && config.Manifest.Embeddings.Provider != "" {
		threshold := int(0.9 * float64(embed.DefaultMaxBytes))

		stats, statsErr := config.Embeddings.Stats(threshold)

		if statsErr != nil {
			return nil, statsErr
		}

		report.EmbedStats = &EmbedStatsReport{
			TotalNodes:   stats.TotalNodes,
			TotalChunks:  stats.TotalChunks,
			MeanChunks:   stats.MeanChunks,
			MedianChunks: stats.MedianChunks,
			MaxChunks:    stats.MaxChunks,
			TopByChunks:  stats.TopByChunks,
		}

		for _, info := range stats.LargeChunks {
			report.Issues = append(report.Issues, Issue{
				Kind:    IssueEmbedLargeChunk,
				NodeID:  info.NodeID,
				Message: fmt.Sprintf("chunk %d body is %d bytes (>= %d threshold, chunker MaxBytes %d)", info.ChunkIdx, info.BodyLen, threshold, embed.DefaultMaxBytes),
			})
		}

		if config.Nodes != nil {
			noChunks, noChunksErr := findNoChunkNodes(config.Nodes, config.Embeddings, config.EmbedQueue)

			if noChunksErr != nil {
				return nil, noChunksErr
			}

			report.Issues = append(report.Issues, noChunks...)
		}
	}

	if config.Nodes != nil {
		pane, paneErr := computeSubUnitPane(config)

		if paneErr != nil {
			return nil, paneErr
		}

		if pane != nil {
			pane.ReservedNameConflicts = len(report.SubUnitConflicts)
			report.SubUnitPane = pane

			// Manifest opt-out + stale rows = dirty index warning.
			if config.Manifest != nil && !config.Manifest.SubUnitsEnabled() && pane.Total > 0 {
				report.Issues = append(report.Issues, Issue{
					Kind:    IssueSubUnitsDisabledDirty,
					Message: fmt.Sprintf("sub-units disabled but index contains %d sub-unit rows; run `tusk reindex --force` to clean up.", pane.Total),
				})
			}
		}
	}

	return report, nil
}

// computeSubUnitPane reads the index for sub-unit health metrics. Returns
// nil when sub-units are disabled by the manifest AND the index has no
// sub-unit rows (the common back-compat case). When stale rows exist
// despite the opt-out, the pane is populated so the CLI/MCP renderer can
// show the counts alongside the dirty-state issue.
func computeSubUnitPane(config Config) (*SubUnitPane, error) {
	// Cheap pre-check: skip the pane entirely on a fresh index with the
	// opt-out in place. Most workspaces never opt out, so the early
	// return only fires for the explicit disable + clean state.
	total, totalErr := config.Nodes.CountSubUnits()

	if totalErr != nil {
		return nil, totalErr
	}

	if total == 0 && config.Manifest != nil && !config.Manifest.SubUnitsEnabled() {
		return nil, nil
	}

	pane := &SubUnitPane{Total: total}

	byKind, kindErr := config.Nodes.CountSubUnitsByKind()

	if kindErr != nil {
		return nil, kindErr
	}

	pane.CountByKind = byKind

	deduped, dedupedErr := config.Nodes.CountDedupedSubUnits()

	if dedupedErr != nil {
		return nil, dedupedErr
	}

	pane.DedupedSubUnits = deduped

	orphans, orphanErr := config.Nodes.CountOrphanedSubUnits()

	if orphanErr != nil {
		return nil, orphanErr
	}

	pane.OrphanedSubUnits = orphans

	oversize, oversizeErr := config.Nodes.CountOversizeSubUnitPayloads(embed.DefaultMaxBytes)

	if oversizeErr != nil {
		return nil, oversizeErr
	}

	pane.OversizeEmbedPayloads = oversize

	// Embed queue split. Walk the pending ids and bucket on whether the
	// id is a sub-unit (`#` separator) or a file row.
	if config.EmbedQueue != nil {
		queued, listErr := config.EmbedQueue.ListNodeIDs()

		if listErr != nil {
			return nil, fmt.Errorf("doctor: list embed queue ids: %w", listErr)
		}

		for _, id := range queued {
			if strings.Contains(id, "#") {
				pane.EmbedQueueSubUnits++
			} else {
				pane.EmbedQueueFiles++
			}
		}
	}

	return pane, nil
}

// CheckPinnedNodes returns the IDs declared under [context.pinned] that
// do not resolve to a node in the index. Returns nil for nil inputs or
// when the manifest declares no Context block. Used by Run to populate
// Report.MissingPinnedIDs and surface one Issue per missing ID.
//
// Pinned IDs are validated at doctor-run time rather than manifest-load
// time because they depend on the live index (a node may have been
// renamed or deleted after the manifest was last edited).
func CheckPinnedNodes(loaded *manifest.Manifest, nodes *index.NodeRepo) []string {
	if loaded == nil || loaded.Context == nil || nodes == nil {
		return nil
	}

	if len(loaded.Context.Pinned) == 0 {
		return nil
	}

	var missing []string

	for _, id := range loaded.Context.Pinned {
		if _, getErr := nodes.Get(id); getErr != nil {
			missing = append(missing, id)
		}
	}

	return missing
}

// renderWorkflowDriftMessage formats the Issue message for a workflow drift
// row. It prefers the rendered Detail persisted by the producer (which carries
// the real rejection code's full text); rows written before the detail column
// existed fall back to the legacy "not a declared state" rendering.
func renderWorkflowDriftMessage(row index.WorkflowDriftRow) string {
	if row.Detail != "" {
		return row.Detail
	}

	return fmt.Sprintf("workflow %q: status %q is not a declared state for property %q",
		row.PackInstance, row.ObservedStatus, row.Property)
}

// renderPropertyDriftMessage formats the Issue message for a property drift
// row per spec §7.3.
func renderPropertyDriftMessage(row index.PropertyDriftRow) string {
	switch row.Kind {
	case IssueUndeclaredProperty:
		return fmt.Sprintf("node-types: property %q not declared on type %q", row.Property, row.NodeType)
	case IssueTypeMismatch:
		return fmt.Sprintf("node-types: property %q — %s", row.Property, row.Details)
	case IssueRequiredMissing:
		return fmt.Sprintf("node-types: required property %q missing on type %q", row.Property, row.NodeType)
	case IssueEnumViolation:
		return fmt.Sprintf("node-types: property %q — %s", row.Property, row.Details)
	case IssueRefDangling, IssueRefAmbiguous, IssueRefTypeMismatch, IssueRefCycle:
		return formatRef(row)
	default:
		return fmt.Sprintf("node-types: %s on property %q", row.Kind, row.Property)
	}
}

// formatRef renders the Issue message for the four ref-resolution drift kinds.
// Their row.Details JSON shapes use disjoint keys, so one best-effort unmarshal
// into a superset struct serves every kind; a switch on row.Kind reproduces each
// per-kind message verbatim. Called only for the four ref kinds, so the dangling
// form is the default.
func formatRef(row index.PropertyDriftRow) string {
	var details struct {
		Value      string   `json:"value"`
		To         string   `json:"to"`
		Candidates []string `json:"candidates"`
		ActualType string   `json:"actual_type"`
		Path       []string `json:"path"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	switch row.Kind {
	case IssueRefAmbiguous:
		return fmt.Sprintf("node-types: ref property %q value %q matches multiple %q candidates: %s",
			row.Property, details.Value, details.To, strings.Join(details.Candidates, ", "))
	case IssueRefTypeMismatch:
		return fmt.Sprintf("node-types: ref property %q value %q target type %q does not match required %q",
			row.Property, details.Value, details.ActualType, details.To)
	case IssueRefCycle:
		return fmt.Sprintf("node-types: ref property %q forms a cycle: %s",
			row.Property, strings.Join(details.Path, " → "))
	default: // IssueRefDangling
		return fmt.Sprintf("node-types: ref property %q value %q did not resolve to any %q", row.Property, details.Value, details.To)
	}
}

// isEmbeddableSubUnit reports whether a sub-unit node-type is embedded. Section
// sub-units are aggregated from their descendants at query time and are never
// embedded, so they must not be flagged as missing embeddings (#513).
func isEmbeddableSubUnit(typeName string) bool {
	switch typeName {
	case "paragraph", "list-item", "code-block", "blockquote", "table-cell":
		return true
	default:
		return false
	}
}

// findNoChunkNodes returns an Issue for every indexed node that should have an
// embedding but has none and is not pending. File rows are always checked;
// sub-unit rows are checked only when their kind is embeddable (sections are
// skipped — they are aggregated, not embedded).
func findNoChunkNodes(nodes *index.NodeRepo, embeddings *index.EmbeddingRepo, queue *index.EmbedQueueRepo) ([]Issue, error) {
	indexed, listErr := nodes.List(index.ListFilter{})

	if listErr != nil {
		return nil, fmt.Errorf("doctor: list nodes: %w", listErr)
	}

	embeddedIDs, embeddedErr := embeddings.ListNodeIDs()

	if embeddedErr != nil {
		return nil, fmt.Errorf("doctor: list embedded nodes: %w", embeddedErr)
	}

	embeddedSet := make(map[string]struct{}, len(embeddedIDs))

	for _, id := range embeddedIDs {
		embeddedSet[id] = struct{}{}
	}

	pendingSet := map[string]struct{}{}

	if queue != nil {
		pendingIDs, pendingErr := queue.ListNodeIDs()

		if pendingErr != nil {
			return nil, fmt.Errorf("doctor: list pending: %w", pendingErr)
		}

		for _, id := range pendingIDs {
			pendingSet[id] = struct{}{}
		}
	}

	var issues []Issue

	for _, row := range indexed {
		// Sub-unit rows with a non-embeddable kind (sections) are never
		// embedded by design — skip them so they aren't false positives.
		if row.ParentID.Valid && !isEmbeddableSubUnit(row.Type) {
			continue
		}

		if _, embedded := embeddedSet[row.ID]; embedded {
			continue
		}

		if _, pending := pendingSet[row.ID]; pending {
			continue
		}

		issues = append(issues, Issue{
			Kind:    IssueEmbedNoChunks,
			NodeID:  row.ID,
			Message: "node has no embedding rows",
		})
	}

	return issues, nil
}

// edgeRowLess orders edge rows by (Type, TargetID, SourcePath) — the
// deterministic tail shared by Migrate (within a single source group) and
// LegacyDrift (after its SourceID primary key).
func edgeRowLess(left, right index.EdgeRow) bool {
	if left.Type != right.Type {
		return left.Type < right.Type
	}

	if left.TargetID != right.TargetID {
		return left.TargetID < right.TargetID
	}

	return left.SourcePath < right.SourcePath
}

// isLegacyEdge reports whether an edge row was written under the legacy CLI or
// MCP sentinel source path — the rows Migrate rewrites into frontmatter and
// LegacyDrift surfaces.
func isLegacyEdge(row index.EdgeRow) bool {
	return row.SourcePath == index.CLISourcePath || row.SourcePath == index.MCPSourcePath
}

// Migrate walks every edge row whose source_path is the legacy CLI or MCP
// sentinel (index.CLISourcePath / index.MCPSourcePath), rewrites it into the
// source node's markdown frontmatter, and clears the legacy row from the
// index. Rows whose source markdown file is missing on disk are reported as
// skipped — the row stays in place so the caller does not lose data.
//
// Migrate is idempotent: once the rows have been migrated, subsequent calls
// observe no legacy rows and return an empty report.
//
// Callers MUST hold the workspace lock: Migrate mutates source files and the
// edges table.
func Migrate(config Config) (*MigrationReport, error) {
	report := &MigrationReport{}

	if config.Edges == nil {
		return report, nil
	}

	if config.Root == "" {
		return nil, fmt.Errorf("doctor: Migrate requires Config.Root")
	}

	if config.Manifest == nil {
		return nil, fmt.Errorf("doctor: Migrate requires Config.Manifest")
	}

	all, listErr := config.Edges.ListAll()

	if listErr != nil {
		return nil, fmt.Errorf("doctor: list edges: %w", listErr)
	}

	// Group legacy rows by source ID only. A single source may carry rows
	// under both sentinels (some edges from `tusk edge add`, others from the
	// MCP `tusk_edge_add` tool); we want to write the markdown and reindex
	// once per source, then clear each sentinel path that actually had rows.
	groups := map[string][]index.EdgeRow{}
	sourcePaths := map[string]map[string]struct{}{}

	for _, row := range all {
		if !isLegacyEdge(row) {
			continue
		}

		groups[row.SourceID] = append(groups[row.SourceID], row)

		if sourcePaths[row.SourceID] == nil {
			sourcePaths[row.SourceID] = map[string]struct{}{}
		}

		sourcePaths[row.SourceID][row.SourcePath] = struct{}{}
	}

	if len(groups) == 0 {
		return report, nil
	}

	// Stable ordering so the report is deterministic across runs.
	orderedSourceIDs := make([]string, 0, len(groups))

	for sourceID := range groups {
		orderedSourceIDs = append(orderedSourceIDs, sourceID)
	}

	sort.Strings(orderedSourceIDs)

	for _, sourceID := range orderedSourceIDs {
		rows := groups[sourceID]
		sourcePath := filepath.Join(config.Root, sourceID+".md")

		if _, statErr := os.Stat(sourcePath); statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				for _, row := range rows {
					report.Skipped = append(report.Skipped,
						fmt.Sprintf("%s [%s]: %s → %s (source file %s.md not found)",
							row.Type, row.SourcePath, row.SourceID, row.TargetID, row.SourceID))
				}

				continue
			}

			return nil, fmt.Errorf("doctor: stat %s: %w", sourcePath, statErr)
		}

		sort.Slice(rows, func(left, right int) bool {
			return edgeRowLess(rows[left], rows[right])
		})

		for _, row := range rows {
			if writeErr := node.AddEdgeToFrontmatter(config.Root, row.SourceID, row.Type, row.TargetID, config.Manifest.EdgeTypes); writeErr != nil {
				return nil, fmt.Errorf("doctor: migrate %s %s→%s: %w", row.Type, row.SourceID, row.TargetID, writeErr)
			}

			report.Migrated = append(report.Migrated,
				fmt.Sprintf("%s [%s]: %s → %s", row.Type, row.SourcePath, row.SourceID, row.TargetID))
		}

		if reindexErr := node.ReindexSource(config.Root, config.Edges, config.Manifest.EdgeTypes, config.Manifest.NodeTypes, sourceID); reindexErr != nil {
			return nil, fmt.Errorf("doctor: reindex %s: %w", sourceID, reindexErr)
		}

		// Clear each sentinel path that actually had rows for this source.
		// Sort for deterministic output ordering on errors.
		seenPaths := make([]string, 0, len(sourcePaths[sourceID]))

		for path := range sourcePaths[sourceID] {
			seenPaths = append(seenPaths, path)
		}

		sort.Strings(seenPaths)

		for _, path := range seenPaths {
			if clearErr := config.Edges.UpsertAll(sourceID, path, nil); clearErr != nil {
				return nil, fmt.Errorf("doctor: clear legacy %s rows for %s: %w", path, sourceID, clearErr)
			}
		}
	}

	return report, nil
}

// LegacyDrift returns one Issue per legacy CLI/MCP edge row currently in the
// index. Designed to be called instead of Migrate when --no-migrate is in
// effect, so users still get an actionable signal about pending migrations.
//
// Rows are emitted in a deterministic order (source ID, then type, then target)
// so test assertions stay stable across runs.
func LegacyDrift(config Config) ([]Issue, error) {
	if config.Edges == nil {
		return nil, nil
	}

	all, listErr := config.Edges.ListAll()

	if listErr != nil {
		return nil, fmt.Errorf("doctor: list edges: %w", listErr)
	}

	legacy := make([]index.EdgeRow, 0)

	for _, row := range all {
		if !isLegacyEdge(row) {
			continue
		}

		legacy = append(legacy, row)
	}

	sort.Slice(legacy, func(left, right int) bool {
		if legacy[left].SourceID != legacy[right].SourceID {
			return legacy[left].SourceID < legacy[right].SourceID
		}

		return edgeRowLess(legacy[left], legacy[right])
	})

	issues := make([]Issue, 0, len(legacy))

	for _, row := range legacy {
		var kind string

		switch row.SourcePath {
		case index.CLISourcePath:
			kind = IssueLegacyCLIEdge
		case index.MCPSourcePath:
			kind = IssueLegacyMCPEdge
		default:
			continue
		}

		issues = append(issues, Issue{
			Kind:   kind,
			NodeID: row.SourceID,
			Message: fmt.Sprintf("%s: %s → %s (run `tusk doctor` without --no-migrate to migrate into source frontmatter)",
				row.Type, row.SourceID, row.TargetID),
		})
	}

	return issues, nil
}

// newDanglingEdgeIssue builds the Issue for an edge whose target_id has no node
// row. Shared by findDanglingEdges' cache-hit-negative and cache-miss branches.
func newDanglingEdgeIssue(edge index.EdgeRow) Issue {
	return Issue{
		Kind:    IssueDanglingEdge,
		NodeID:  edge.SourceID,
		Message: fmt.Sprintf("edge %q -> %q (target missing)", edge.Type, edge.TargetID),
	}
}

// findDanglingEdges scans every edge and flags those whose target_id has no
// node row.
func findDanglingEdges(nodes *index.NodeRepo, edges *index.EdgeRepo) ([]Issue, error) {
	allEdges, listErr := edges.ListAll()

	if listErr != nil {
		return nil, listErr
	}

	var issues []Issue

	// Cache existence checks: NodeRepo.Get returns an error when the row is
	// missing; cache positive lookups in a set, query on first miss.
	resolved := map[string]bool{}

	for _, edge := range allEdges {
		if cached, hit := resolved[edge.TargetID]; hit {
			if cached {
				continue
			}

			issues = append(issues, newDanglingEdgeIssue(edge))

			continue
		}

		if _, getErr := nodes.Get(edge.TargetID); getErr != nil {
			resolved[edge.TargetID] = false

			issues = append(issues, newDanglingEdgeIssue(edge))

			continue
		}

		resolved[edge.TargetID] = true
	}

	return issues, nil
}
