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
)

// Issue is a single problem the doctor surfaced.
type Issue struct {
	Kind    string
	NodeID  string
	Message string
}

// Report is the doctor's verdict.
type Report struct {
	Issues          []Issue
	EmbedQueueDepth int
	EmbedStats      *EmbedStatsReport
	// AliasErrors mirrors Manifest.AliasErrors for callers that want the
	// typed list (CLI, MCP) instead of parsing them back out of Issues.
	AliasErrors []manifest.AliasError
	// ContextErrors mirrors Manifest.ContextErrors so CLI and MCP can
	// surface invalid [context] declarations without re-parsing Issues.
	ContextErrors []manifest.ContextError
	// MissingPinnedIDs lists [context.pinned] entries that do not
	// resolve to a node in the current index. Computed at Run time.
	MissingPinnedIDs []string
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
		depth, depthErr := config.EmbedQueue.Depth()

		if depthErr != nil {
			return nil, depthErr
		}

		report.EmbedQueueDepth = depth
	}

	if config.WorkflowDrift != nil {
		drift, listErr := config.WorkflowDrift.ListAll()

		if listErr != nil {
			return nil, listErr
		}

		for _, row := range drift {
			report.Issues = append(report.Issues, Issue{
				Kind:   IssueWorkflowViolation,
				NodeID: row.NodeID,
				Message: fmt.Sprintf("workflow %q: status %q is not a declared state for property %q",
					row.PackInstance, row.ObservedStatus, row.Property),
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

	return report, nil
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
	case IssueRefDangling:
		return formatRefDangling(row)
	case IssueRefAmbiguous:
		return formatRefAmbiguous(row)
	case IssueRefTypeMismatch:
		return formatRefTypeMismatch(row)
	case IssueRefCycle:
		return formatRefCycle(row)
	default:
		return fmt.Sprintf("node-types: %s on property %q", row.Kind, row.Property)
	}
}

func formatRefDangling(row index.PropertyDriftRow) string {
	var details struct {
		Value string `json:"value"`
		To    string `json:"to"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	return fmt.Sprintf("node-types: ref property %q value %q did not resolve to any %q", row.Property, details.Value, details.To)
}

func formatRefAmbiguous(row index.PropertyDriftRow) string {
	var details struct {
		Value      string   `json:"value"`
		To         string   `json:"to"`
		Candidates []string `json:"candidates"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	return fmt.Sprintf("node-types: ref property %q value %q matches multiple %q candidates: %s",
		row.Property, details.Value, details.To, strings.Join(details.Candidates, ", "))
}

func formatRefTypeMismatch(row index.PropertyDriftRow) string {
	var details struct {
		Value      string `json:"value"`
		To         string `json:"to"`
		ActualType string `json:"actual_type"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	return fmt.Sprintf("node-types: ref property %q value %q target type %q does not match required %q",
		row.Property, details.Value, details.ActualType, details.To)
}

func formatRefCycle(row index.PropertyDriftRow) string {
	var details struct {
		Path []string `json:"path"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	return fmt.Sprintf("node-types: ref property %q forms a cycle: %s",
		row.Property, strings.Join(details.Path, " → "))
}

// findNoChunkNodes returns an Issue for every indexed node that has no
// embedding rows and is not pending in the embed queue.
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
		if row.SourcePath != index.CLISourcePath && row.SourcePath != index.MCPSourcePath {
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
			if rows[left].Type != rows[right].Type {
				return rows[left].Type < rows[right].Type
			}

			if rows[left].TargetID != rows[right].TargetID {
				return rows[left].TargetID < rows[right].TargetID
			}

			return rows[left].SourcePath < rows[right].SourcePath
		})

		for _, row := range rows {
			if writeErr := node.AddEdgeToFrontmatter(config.Root, row.SourceID, row.Type, row.TargetID, config.Manifest.EdgeTypes); writeErr != nil {
				return nil, fmt.Errorf("doctor: migrate %s %s→%s: %w", row.Type, row.SourceID, row.TargetID, writeErr)
			}

			report.Migrated = append(report.Migrated,
				fmt.Sprintf("%s [%s]: %s → %s", row.Type, row.SourcePath, row.SourceID, row.TargetID))
		}

		if reindexErr := node.ReindexSource(config.Root, config.Edges, config.Manifest.EdgeTypes, sourceID); reindexErr != nil {
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
		if row.SourcePath != index.CLISourcePath && row.SourcePath != index.MCPSourcePath {
			continue
		}

		legacy = append(legacy, row)
	}

	sort.Slice(legacy, func(left, right int) bool {
		if legacy[left].SourceID != legacy[right].SourceID {
			return legacy[left].SourceID < legacy[right].SourceID
		}

		if legacy[left].Type != legacy[right].Type {
			return legacy[left].Type < legacy[right].Type
		}

		if legacy[left].TargetID != legacy[right].TargetID {
			return legacy[left].TargetID < legacy[right].TargetID
		}

		return legacy[left].SourcePath < legacy[right].SourcePath
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

			issues = append(issues, Issue{
				Kind:    IssueDanglingEdge,
				NodeID:  edge.SourceID,
				Message: fmt.Sprintf("edge %q -> %q (target missing)", edge.Type, edge.TargetID),
			})

			continue
		}

		if _, getErr := nodes.Get(edge.TargetID); getErr != nil {
			resolved[edge.TargetID] = false

			issues = append(issues, Issue{
				Kind:    IssueDanglingEdge,
				NodeID:  edge.SourceID,
				Message: fmt.Sprintf("edge %q -> %q (target missing)", edge.Type, edge.TargetID),
			})

			continue
		}

		resolved[edge.TargetID] = true
	}

	return issues, nil
}
