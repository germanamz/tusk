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

	// IssueEmbeddingDrift surfaces embedding model/dim drift: the workspace's
	// configured embeddings.model / embeddings.dim no longer match the model
	// or dim of the vectors stored in the index. Because the vector store is
	// keyed (content_hash, model) and CosineSimilarity returns 0 on a dim
	// mismatch, the stale vectors silently vanish from query results. Doctor
	// does NOT auto-rebuild; the user must run `tusk reset` to re-embed.
	IssueEmbeddingDrift = "embedding-drift"

	IssueLegacyCLIEdge = "legacy-cli-edge"
	IssueLegacyMCPEdge = "legacy-mcp-edge"

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

	// IssueGraphExpansionInvalidEdge surfaces an entry in
	// [query.graph-expansion] edge-types that is not a valid type
	// reference under the typeref grammar (e.g. "Refs"). The query path
	// parses the same list with typeref.ParseMany, so a malformed entry
	// makes EVERY `--semantic` query hard-fail — this is not a
	// silently-skipped unknown edge, it is a hard misconfiguration.
	IssueGraphExpansionInvalidEdge = "graph-expansion-invalid-edge"

	// IssueGraphExpansionNoEdges surfaces the no-op configuration where
	// [query.graph-expansion] enabled=true but edge-types is an explicit
	// empty list. The walker adds no neighbors, so the feature contributes
	// nothing — the sibling of the weight=0 no-op.
	IssueGraphExpansionNoEdges = "graph-expansion-no-edges"
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

	// Alias / context / sub-unit-conflict errors and missing pinned IDs
	// each flow through their own typed Report field — AliasErrors,
	// ContextErrors, SubUnitConflicts, MissingPinnedIDs — and every
	// surface renders them from there. They are deliberately NOT mirrored
	// into Report.Issues; doing so double-rendered every such error.
	if config.Manifest != nil && len(config.Manifest.AliasErrors) > 0 {
		report.AliasErrors = append(report.AliasErrors, config.Manifest.AliasErrors...)
	}

	if config.Manifest != nil && len(config.Manifest.ContextErrors) > 0 {
		report.ContextErrors = append(report.ContextErrors, config.Manifest.ContextErrors...)
	}

	if config.Manifest != nil {
		pane, issues := computeGraphExpansionPane(config.Manifest)

		report.GraphExpansion = pane
		report.Issues = append(report.Issues, issues...)
	}

	if config.Manifest != nil && config.Nodes != nil && config.Manifest.Context != nil {
		missing := CheckPinnedNodes(config.Manifest, config.Nodes)

		if len(missing) > 0 {
			report.MissingPinnedIDs = missing
		}
	}

	// Independent issue-only checks: each scans one repo and contributes
	// Issues without reading anything an earlier block computed. Order
	// among them is cosmetic (it fixes the Issue listing order).
	for _, check := range []func(Config) ([]Issue, error){
		checkDanglingEdges,
		checkWorkflowDrift,
		checkPropertyDrift,
		checkEmbeddingDrift,
		checkEmbedRetries,
	} {
		issues, checkErr := check(config)

		if checkErr != nil {
			return nil, checkErr
		}

		report.Issues = append(report.Issues, issues...)
	}

	if queueErr := populateQueueDepths(config, report); queueErr != nil {
		return nil, queueErr
	}

	if embedErr := populateEmbedStats(config, report); embedErr != nil {
		return nil, embedErr
	}

	// Sub-unit pane: NOT independent. The dirty-state warning depends on
	// the pane's freshly-computed Total, so it stays an explicit call after
	// the independent checks above.
	if paneErr := populateSubUnitPane(config, report); paneErr != nil {
		return nil, paneErr
	}

	return report, nil
}

// checkDanglingEdges flags every edge whose target_id has no node row. No-op
// when either repo is absent.
func checkDanglingEdges(config Config) ([]Issue, error) {
	if config.Edges == nil || config.Nodes == nil {
		return nil, nil
	}

	return findDanglingEdges(config.Nodes, config.Edges)
}

// liveNodeIDs returns the subset of ids that resolve to a node row, so drift
// checks can skip rows orphaned by a delete or rename. When the node repo is
// absent the second return is false: callers keep every row rather than drop
// all of them (there is nothing to check existence against).
//
// Drift is only ever recorded while validating a live node, so a row whose
// node has since been deleted or renamed away is an orphan with no repair path
// — pointing users at ghost ids "forever" is exactly the noise #685 flags.
// Reindex sweeps these rows (DeleteOrphans), but `tusk node delete` does not
// reindex and files can vanish out of band, so doctor filters them at read
// time too rather than trust that a sweep has already run.
func liveNodeIDs(nodes *index.NodeRepo, ids []string) (map[string]struct{}, bool, error) {
	if nodes == nil {
		return nil, false, nil
	}

	rows, listErr := nodes.ListByIDs(ids)

	if listErr != nil {
		return nil, false, listErr
	}

	live := make(map[string]struct{}, len(rows))

	for _, row := range rows {
		live[row.ID] = struct{}{}
	}

	return live, true, nil
}

// checkWorkflowDrift surfaces one workflow-violation Issue per persisted drift
// row whose node still exists. No-op when the drift repo is absent; orphaned
// rows are skipped (see liveNodeIDs).
func checkWorkflowDrift(config Config) ([]Issue, error) {
	if config.WorkflowDrift == nil {
		return nil, nil
	}

	drift, listErr := config.WorkflowDrift.ListAll()

	if listErr != nil {
		return nil, listErr
	}

	ids := make([]string, 0, len(drift))

	for _, row := range drift {
		ids = append(ids, row.NodeID)
	}

	live, filterOrphans, liveErr := liveNodeIDs(config.Nodes, ids)

	if liveErr != nil {
		return nil, liveErr
	}

	issues := make([]Issue, 0, len(drift))

	for _, row := range drift {
		if filterOrphans {
			if _, ok := live[row.NodeID]; !ok {
				continue
			}
		}

		issues = append(issues, Issue{
			Kind:    IssueWorkflowViolation,
			NodeID:  row.NodeID,
			Message: renderWorkflowDriftMessage(row),
		})
	}

	return issues, nil
}

// checkPropertyDrift surfaces one Issue per persisted property-drift row whose
// node still exists, carrying the row's own Kind. No-op when the drift repo is
// absent; orphaned rows are skipped (see liveNodeIDs).
func checkPropertyDrift(config Config) ([]Issue, error) {
	if config.PropertyDrift == nil {
		return nil, nil
	}

	propDrift, listErr := config.PropertyDrift.ListAll()

	if listErr != nil {
		return nil, listErr
	}

	ids := make([]string, 0, len(propDrift))

	for _, row := range propDrift {
		ids = append(ids, row.NodeID)
	}

	live, filterOrphans, liveErr := liveNodeIDs(config.Nodes, ids)

	if liveErr != nil {
		return nil, liveErr
	}

	issues := make([]Issue, 0, len(propDrift))

	for _, row := range propDrift {
		if filterOrphans {
			if _, ok := live[row.NodeID]; !ok {
				continue
			}
		}

		issues = append(issues, Issue{
			Kind:    row.Kind,
			NodeID:  row.NodeID,
			Message: renderPropertyDriftMessage(row),
		})
	}

	return issues, nil
}

// checkEmbeddingDrift flags embedding model/dim drift: stored vectors whose
// model or dim differs from the workspace's configured embeddings.model /
// embeddings.dim. Editing either setting silently splits the (content_hash,
// model)-keyed vector store — old vectors keep their original key and drop
// out of query results, and CosineSimilarity returns 0 on a dim mismatch —
// so semantic recall becomes quietly incomplete with no rebuild trigger.
// The check is read-only and never rebuilds; it advises `tusk reset`. No-op
// when no embeddings repo is configured or [embeddings] is absent from the
// manifest (Provider == ""): there is nothing to compare against.
func checkEmbeddingDrift(config Config) ([]Issue, error) {
	if config.Embeddings == nil || config.Manifest == nil || config.Manifest.Embeddings.Provider == "" {
		return nil, nil
	}

	stored, distinctErr := config.Embeddings.DistinctModelDims()

	if distinctErr != nil {
		return nil, fmt.Errorf("doctor: distinct embedding model/dims: %w", distinctErr)
	}

	configuredModel := config.Manifest.Embeddings.Model
	configuredDim := config.Manifest.Embeddings.Dim

	var issues []Issue

	for _, pair := range stored {
		if pair.Model == configuredModel && pair.Dim == configuredDim {
			continue
		}

		issues = append(issues, Issue{
			Kind: IssueEmbeddingDrift,
			Message: fmt.Sprintf("stored embeddings use model %q (dim %d) but the workspace is configured for model %q (dim %d); the configured embedder no longer matches the stored vectors, so semantic results are silently incomplete — run `tusk reset` (`tusk_reset`) to drop and re-embed.",
				pair.Model, pair.Dim, configuredModel, configuredDim),
		})
	}

	return issues, nil
}

// checkEmbedRetries surfaces embed-queue rows that have failed at least once
// (attempts > 0) with their attempt count and last error. Without this, a
// persistently failing embedder (wrong endpoint, 404 model, transport abort)
// is invisible to doctor: the row looks like an ordinary pending job, and once
// the attempts cap drops it the failure survives only as a generic
// embed-no-chunks with no cause. Emits the declared IssueEmbedRetry kind that
// the MCP tusk_doctor tool advertises ("embed-queue retries") but that no code
// path wrote before. No-op when the queue repo is absent.
func checkEmbedRetries(config Config) ([]Issue, error) {
	if config.EmbedQueue == nil {
		return nil, nil
	}

	retrying, listErr := config.EmbedQueue.ListRetrying()

	if listErr != nil {
		return nil, listErr
	}

	issues := make([]Issue, 0, len(retrying))

	for _, row := range retrying {
		message := fmt.Sprintf("embed queue row has failed %d attempt(s) and is still pending", row.Attempts)

		if row.LastError != "" {
			message += fmt.Sprintf("; last error: %s", row.LastError)
		}

		issues = append(issues, Issue{
			Kind:    IssueEmbedRetry,
			NodeID:  row.NodeID,
			Message: message,
		})
	}

	return issues, nil
}

// populateQueueDepths fills report.EmbedQueueDepth / ReindexQueueDepth from the
// embed queue. No-op when the queue repo is absent.
func populateQueueDepths(config Config, report *Report) error {
	if config.EmbedQueue == nil {
		return nil
	}

	embedDepth, embedDepthErr := config.EmbedQueue.DepthByKind("embed")

	if embedDepthErr != nil {
		return embedDepthErr
	}

	report.EmbedQueueDepth = embedDepth

	reindexDepth, reindexDepthErr := config.EmbedQueue.DepthByKind("reindex")

	if reindexDepthErr != nil {
		return reindexDepthErr
	}

	report.ReindexQueueDepth = reindexDepth

	return nil
}

// populateEmbedStats fills report.EmbedStats and appends the large-chunk /
// no-chunk Issues. No-op unless an embeddings repo and an embedding provider
// are both configured.
func populateEmbedStats(config Config, report *Report) error {
	if config.Embeddings == nil || config.Manifest == nil || config.Manifest.Embeddings.Provider == "" {
		return nil
	}

	threshold := int(0.9 * float64(embed.DefaultMaxBytes))

	stats, statsErr := config.Embeddings.Stats(threshold)

	if statsErr != nil {
		return statsErr
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
			return noChunksErr
		}

		report.Issues = append(report.Issues, noChunks...)
	}

	return nil
}

// populateSubUnitPane computes the sub-unit health pane and the dirty-state
// warning. It is deliberately NOT one of the independent checks: it keys the
// dirty-state Issue off the pane's freshly-computed Total. No-op when the node
// repo is absent or the pane is nil (opt-out + clean index).
func populateSubUnitPane(config Config, report *Report) error {
	if config.Nodes == nil {
		return nil
	}

	pane, paneErr := computeSubUnitPane(config)

	if paneErr != nil {
		return paneErr
	}

	if pane == nil {
		return nil
	}

	report.SubUnitPane = pane

	// Manifest opt-out + stale rows = dirty index warning.
	if config.Manifest != nil && !config.Manifest.SubUnitsEnabled() && pane.Total > 0 {
		report.Issues = append(report.Issues, Issue{
			Kind:    IssueSubUnitsDisabledDirty,
			Message: fmt.Sprintf("sub-units disabled but index contains %d sub-unit rows; run `tusk reindex --force` to clean up.", pane.Total),
		})
	}

	return nil
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
		if row.ParentID.Valid {
			// Sub-unit rows with a non-embeddable kind (sections) are never
			// embedded by design — skip them so they aren't false positives.
			if !isEmbeddableSubUnit(row.Type) {
				continue
			}

			// A sub-unit with an empty embed payload (e.g. a text-less "- [ ]"
			// task item) can never embed — the drain drops it by design. It
			// carries no content to embed, so a missing embedding is not a
			// failure; flagging it turns embed-no-chunks into a permanent false
			// positive that a healthy vault can never clear (#682 item 5).
			if row.EmbedPayload.String == "" {
				continue
			}
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

	// Partition legacy rows by source ID, splitting migratable rows from ones
	// whose edge type is no longer declared in the manifest. A single source
	// may carry rows under both sentinels (some from `tusk edge add`, others
	// from the MCP `tusk_edge_add` tool); we write the markdown and reindex
	// once per source, then clear each sentinel path that actually had rows.
	//
	// An un-migratable row (undeclared edge type) CANNOT be written to
	// frontmatter — frontmatter edges must be declared — so migrating it is
	// impossible. Previously such a row aborted the entire diagnostic run with
	// a hard error and produced NO report at all, dying on exactly the drift
	// doctor exists to surface (#685). Instead, record it as skipped, leave it
	// in the index, and migrate everything else.
	migratable := map[string][]index.EdgeRow{}
	unmigratable := map[string][]index.EdgeRow{}
	sourcePaths := map[string]map[string]struct{}{}

	for _, row := range all {
		if !isLegacyEdge(row) {
			continue
		}

		if _, declared := config.Manifest.EdgeTypes[row.Type]; !declared {
			unmigratable[row.SourceID] = append(unmigratable[row.SourceID], row)

			continue
		}

		migratable[row.SourceID] = append(migratable[row.SourceID], row)

		if sourcePaths[row.SourceID] == nil {
			sourcePaths[row.SourceID] = map[string]struct{}{}
		}

		sourcePaths[row.SourceID][row.SourcePath] = struct{}{}
	}

	// Surface every un-migratable row as skipped, in a deterministic order.
	// These rows stay in the index untouched.
	report.Skipped = append(report.Skipped, unmigratableSkips(unmigratable)...)

	if len(migratable) == 0 {
		return report, nil
	}

	// Stable ordering so the report is deterministic across runs.
	orderedSourceIDs := make([]string, 0, len(migratable))

	for sourceID := range migratable {
		orderedSourceIDs = append(orderedSourceIDs, sourceID)
	}

	sort.Strings(orderedSourceIDs)

	for _, sourceID := range orderedSourceIDs {
		rows := migratable[sourceID]
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

		// keepByPath holds rows that must survive the sentinel-path clear
		// below: un-migratable rows for this source, plus any migratable row
		// whose frontmatter write failed (e.g. a cardinality conflict). Without
		// this, a mixed path would silently drop the rows we could not migrate.
		keepByPath := map[string][]index.EdgeRow{}

		for _, row := range unmigratable[sourceID] {
			keepByPath[row.SourcePath] = append(keepByPath[row.SourcePath], row)
		}

		for _, row := range rows {
			if writeErr := node.AddEdgeToFrontmatter(config.Root, row.SourceID, row.Type, row.TargetID, config.Manifest.EdgeTypes, config.Manifest.NodeTypes); writeErr != nil {
				report.Skipped = append(report.Skipped,
					fmt.Sprintf("%s [%s]: %s → %s (cannot migrate: %v)",
						row.Type, row.SourcePath, row.SourceID, row.TargetID, writeErr))
				keepByPath[row.SourcePath] = append(keepByPath[row.SourcePath], row)

				continue
			}

			report.Migrated = append(report.Migrated,
				fmt.Sprintf("%s [%s]: %s → %s", row.Type, row.SourcePath, row.SourceID, row.TargetID))
		}

		if reindexErr := node.ReindexSource(config.Root, config.Nodes, config.Edges, node.NewIndexRefLookup(config.Nodes), config.Manifest.EdgeTypes, config.Manifest.NodeTypes, sourceID); reindexErr != nil {
			return nil, fmt.Errorf("doctor: reindex %s: %w", sourceID, reindexErr)
		}

		// Clear each sentinel path that actually had migratable rows for this
		// source, re-inserting the rows we must preserve. Sort for
		// deterministic output ordering on errors.
		seenPaths := make([]string, 0, len(sourcePaths[sourceID]))

		for path := range sourcePaths[sourceID] {
			seenPaths = append(seenPaths, path)
		}

		sort.Strings(seenPaths)

		for _, path := range seenPaths {
			if clearErr := config.Edges.UpsertAll(sourceID, path, keepByPath[path]); clearErr != nil {
				return nil, fmt.Errorf("doctor: clear legacy %s rows for %s: %w", path, sourceID, clearErr)
			}
		}
	}

	return report, nil
}

// unmigratableSkips renders one deterministic skipped line per legacy edge row
// whose type is no longer declared in the manifest. Such rows cannot be written
// to frontmatter (frontmatter edges must be declared), so they are surfaced and
// left in place — the user declares the type in tusk.toml or removes the row
// with `tusk edge remove`.
func unmigratableSkips(unmigratable map[string][]index.EdgeRow) []string {
	rows := make([]index.EdgeRow, 0)

	for _, group := range unmigratable {
		rows = append(rows, group...)
	}

	sort.Slice(rows, func(left, right int) bool {
		if rows[left].SourceID != rows[right].SourceID {
			return rows[left].SourceID < rows[right].SourceID
		}

		return edgeRowLess(rows[left], rows[right])
	})

	skips := make([]string, 0, len(rows))

	for _, row := range rows {
		skips = append(skips,
			fmt.Sprintf("%s [%s]: %s → %s (edge type %q not declared in manifest; declare it in tusk.toml or run `tusk edge remove` to clear it)",
				row.Type, row.SourcePath, row.SourceID, row.TargetID, row.Type))
	}

	return skips
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

		// A row whose edge type is no longer declared cannot be migrated into
		// frontmatter, so the default "run doctor to migrate" advice would send
		// the user in a circle (the migrate pass skips it). Point them at the
		// real fix instead.
		hint := "run `tusk doctor` without --no-migrate to migrate into source frontmatter"

		if config.Manifest != nil {
			if _, declared := config.Manifest.EdgeTypes[row.Type]; !declared {
				hint = fmt.Sprintf("edge type %q not declared in manifest; declare it in tusk.toml or run `tusk edge remove` to clear it", row.Type)
			}
		}

		issues = append(issues, Issue{
			Kind:    kind,
			NodeID:  row.SourceID,
			Message: fmt.Sprintf("%s: %s → %s (%s)", row.Type, row.SourceID, row.TargetID, hint),
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

	// Collect the distinct target ids, then resolve existence in a single
	// batched lookup instead of one NodeRepo.Get per distinct target.
	targetIDs := make([]string, 0, len(allEdges))
	seen := make(map[string]struct{}, len(allEdges))

	for _, edge := range allEdges {
		if _, ok := seen[edge.TargetID]; ok {
			continue
		}

		seen[edge.TargetID] = struct{}{}
		targetIDs = append(targetIDs, edge.TargetID)
	}

	existingRows, byIDErr := nodes.ListByIDs(targetIDs)

	if byIDErr != nil {
		return nil, byIDErr
	}

	exists := make(map[string]struct{}, len(existingRows))

	for _, row := range existingRows {
		exists[row.ID] = struct{}{}
	}

	var issues []Issue

	// Iterate edges in ListAll order so the issue set and ordering match the
	// prior per-edge existence-check path exactly.
	for _, edge := range allEdges {
		if _, ok := exists[edge.TargetID]; ok {
			continue
		}

		issues = append(issues, newDanglingEdgeIssue(edge))
	}

	return issues, nil
}
