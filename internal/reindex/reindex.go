// Package reindex walks a workspace, parses every markdown and HTML node, and
// brings the index up to date with what is on disk.
package reindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/ignore"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/leaseconfig"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// indexableExts is the single source of truth for which file extensions the
// reindex pipeline treats as content nodes. Consulted by the walk gate, the
// tombstone derivation, and the parse dispatch so the set is declared once.
var indexableExts = map[string]bool{
	".md":   true,
	".html": true,
	".htm":  true,
}

// edgeDerivationVersion tags the edge-derivation semantics baked into this
// binary, stored in meta under edgeDerivationVersionKey. When the stored
// value differs (older binary, or the key predates the mechanism), Run
// forces one full re-process pass so every file's edges converge on the
// current semantics — the incremental mtime+size skip would otherwise
// preserve stale edges indefinitely. Content-addressed embeddings make the
// forced pass cheap: unchanged content re-parses but never re-embeds. Bump
// the value whenever edge derivation changes meaning.
const (
	edgeDerivationVersionKey = "edge_derivation_version"
	edgeDerivationVersion    = "2026-07-09-section-subtree-edges"
)

// nodeIDForPath derives a node id from a workspace-relative path. Markdown
// keeps its historical bare-stem id (strips ".md"); every other indexable
// kind retains its full filename so same-stem files (foo.md / foo.html) never
// collide on the nodes.id PRIMARY KEY (design Decision #12).
func nodeIDForPath(path string) string {
	if filepath.Ext(path) == ".md" {
		return strings.TrimSuffix(path, ".md")
	}

	return path
}

// Config configures Run.
type Config struct {
	Root            string             // workspace root
	Repo            *index.NodeRepo    // node index repository
	Edges           *index.EdgeRepo    // edge index repository (optional; when nil, edges are not written)
	EdgeTypes       manifest.EdgeTypes // declared edge types (optional; when empty, frontmatter edges are not resolved)
	WorkspaceIgnore []string           // patterns from [workspace] ignore in tusk.toml

	// Embedding pipeline (optional). When all four are set, Run drains the
	// embed_queue at the end of the pass by invoking Embedder for each node.
	EmbedQueue    *index.EmbedQueueRepo
	EmbeddingRepo *index.EmbeddingRepo
	Embedder      embed.Embedder
	Chunker       embed.ChunkingStrategy

	// Meta is required. At the start of each pass Run atomically bumps the
	// `reindex_gen` key on Meta and stamps the resulting value on every
	// file_state row it touches; at the end of the pass it records
	// `last_reindex_at` (unix nanoseconds formatted as decimal string).
	Meta *index.MetaRepo

	// FileStates is required. The walker upserts a file_state row for
	// every indexed file, stamping `last_seen_gen` with the current
	// generation so T6.2's generation-based reap can identify rows whose
	// files disappeared between passes.
	FileStates *index.FileStateRepo

	// Behaviors is optional; when set, Run fires the workflow validator in
	// warn mode for each indexed node. Violations are persisted to DriftLog.
	Behaviors node.Behaviors

	// DriftLog is optional; when set alongside Behaviors, Run writes drift rows
	// for validator rejections and clears rows on clean passes.
	DriftLog *index.WorkflowDriftRepo

	// NodeTypes is optional; when set, Run validates each indexed node's
	// properties against the declared node types in warn mode.
	NodeTypes map[string]manifest.NodeType

	// PropertyDrift is optional; when set alongside NodeTypes, Run writes
	// property drift rows and clears rows on clean passes.
	PropertyDrift *index.PropertyDriftRepo

	// Logger is optional; when set, Run emits structured logs to it.
	// Forwarded into embed.DrainConfig.Logger when the embedding pipeline runs.
	Logger *slog.Logger

	// Workers caps concurrent embed calls per node when the embedding pipeline
	// runs. Forwarded to embed.DrainConfig.Workers. Zero means "serial".
	Workers int

	// Manifest carries the workspace manifest. Optional; when nil the
	// reindex pass treats sub-units as disabled regardless of any other
	// flag. When non-nil and Manifest.SubUnitsEnabled() is true, the
	// per-file loop diffs sub-units into the index alongside the file
	// row (Plan 2 Task 3). Edges and EdgeTypes still source from the
	// dedicated Config fields for back-compat with callers that build a
	// reindex config without a manifest (the existing reindex tests).
	Manifest *manifest.Manifest

	// Async controls whether Run drains the reindex queue before returning.
	// Default (false) blocks until every enqueued reindex job is processed
	// by an in-process worker pool — the CLI and rebuild paths rely on this
	// "Run returns ⇒ work done" semantic. Set true for callers that own a
	// long-lived background worker pool (watch/MCP runtime) so Run returns
	// as soon as the walk completes.
	Async bool

	// Force disables the unchanged-file skip: when true, every file is re-read,
	// re-hashed, and re-enqueued even if its mtime+size match the last pass.
	// Default (false) skips files whose mtime AND size are unchanged — a large
	// speedup on incremental reindex. The skip is a performance trade-off, not a
	// correctness guarantee (a content change that preserves both mtime and size
	// would be missed); Force is the escape hatch.
	Force bool
}

// RebuildConfig fills the repo-and-manifest fields a from-scratch index
// rebuild needs: the workspace root, the node/edge/meta/file-state/embed-queue
// repos over idx, and the manifest's declared edge types. It is the shared
// core of the indexopen ReindexFactory used by the CLI and the MCP runtime.
//
// Per-caller divergences (Workers, Logger, Chunker, Embedder, …) are NOT set
// here: callers override the returned config's fields explicitly so the
// differences stay visible at the call site.
func RebuildConfig(root string, idx *index.Index, loaded *manifest.Manifest) Config {
	return Config{
		Root:       root,
		Repo:       index.NewNodeRepo(idx),
		Edges:      index.NewEdgeRepo(idx),
		EdgeTypes:  loaded.EdgeTypes,
		Meta:       index.NewMetaRepo(idx),
		FileStates: index.NewFileStateRepo(idx),
		EmbedQueue: index.NewEmbedQueueRepo(idx),
	}
}

// Report summarizes a reindex pass.
type Report struct {
	Indexed            int // number of node files freshly indexed or refreshed
	Removed            int // number of stale rows deleted (file no longer on disk)
	Skipped            int // number of files skipped (parse error or off-schema)
	WorkflowViolations int // number of workflow validation failures (warn mode)
	PropertyViolations int // number of property validation failures (warn mode)
	RefDangling        int // number of ref_dangling issues surfaced
	RefAmbiguous       int // number of ref_ambiguous issues surfaced
	RefTypeMismatch    int // number of ref_type_mismatch issues surfaced
	RefCycle           int // number of ref_cycle issues surfaced

	// Sub-unit pipeline counters (Plan 2 Task 3). All zero when the
	// workspace's `sub-units` flag is false or when Config.Manifest is
	// nil.
	SubUnitsInserted  int // sub-unit rows freshly written across all files
	SubUnitsDeleted   int // sub-unit rows removed across all files
	SubUnitsReordered int // sub-unit rows whose ordinal changed

	// Generation is the value of `reindex_gen` assigned to this pass.
	// Informational for callers; T6.2 will use it for generation-based
	// orphan reap.
	Generation int64
}

// mergeDrain folds a DrainReport's per-file counters into rep. The fields
// mirror DrainReport.merge; only the counters the per-file path produces are
// accumulated (Removed and Generation are owned by the walk, not the drain).
func (rep *Report) mergeDrain(other DrainReport) {
	rep.Indexed += other.Indexed
	rep.Skipped += other.Skipped
	rep.WorkflowViolations += other.WorkflowViolations
	rep.PropertyViolations += other.PropertyViolations
	rep.RefDangling += other.RefDangling
	rep.RefAmbiguous += other.RefAmbiguous
	rep.RefTypeMismatch += other.RefTypeMismatch
	rep.RefCycle += other.RefCycle
	rep.SubUnitsInserted += other.SubUnitsInserted
	rep.SubUnitsDeleted += other.SubUnitsDeleted
	rep.SubUnitsReordered += other.SubUnitsReordered
}

// Run walks Root, parses every *.md file with valid frontmatter and every
// *.html/*.htm file with a tusk:type meta directive, and upserts or removes
// index rows so the index matches what is on disk. When Edges and EdgeTypes
// are configured, edges are written and removed alongside nodes.
func Run(config Config) (*Report, error) {
	if config.Meta == nil {
		return nil, fmt.Errorf("reindex: Meta is required")
	}

	if config.FileStates == nil {
		return nil, fmt.Errorf("reindex: FileStates is required")
	}

	if config.EmbedQueue == nil {
		return nil, fmt.Errorf("reindex: EmbedQueue is required")
	}

	report := &Report{}

	start := time.Now()

	workerID := index.WorkerID()

	var manifestTTL int

	if config.Manifest != nil {
		manifestTTL = config.Manifest.Lease.TTLSeconds
	}

	leaseTTL := leaseconfig.Resolve(manifestTTL)

	gen, incrErr := config.Meta.Incr("reindex_gen", 1)

	if incrErr != nil {
		return nil, fmt.Errorf("reindex: bump reindex_gen: %w", incrErr)
	}

	report.Generation = gen

	// A derivation-version mismatch means this index was written by a
	// binary with different edge-derivation semantics (e.g. the pre-fix
	// binary that never re-derived section-sourced edges and stamped
	// heading-only section hashes). The incremental mtime+size skip would
	// preserve those fossils forever, so force one full re-process pass;
	// the marker is stamped after the pass succeeds.
	derivationMarker, markerErr := config.Meta.Get(edgeDerivationVersionKey)

	if markerErr != nil {
		return nil, fmt.Errorf("reindex: read %s: %w", edgeDerivationVersionKey, markerErr)
	}

	if derivationMarker != edgeDerivationVersion {
		config.Force = true

		if config.Logger != nil {
			config.Logger.Info("reindex: edge-derivation version changed; forcing full re-process",
				"stored", derivationMarker,
				"current", edgeDerivationVersion,
			)
		}
	}

	if config.Logger != nil {
		config.Logger.Info("reindex walk start",
			"root", config.Root,
			"ignore_patterns_count", len(config.WorkspaceIgnore),
			"generation", gen,
		)
	}

	matcher, matcherErr := ignore.NewMatcher(config.Root, config.WorkspaceIgnore)

	if matcherErr != nil {
		return nil, fmt.Errorf("reindex: build ignore matcher: %w", matcherErr)
	}

	walkErr := filepath.WalkDir(config.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, relErr := filepath.Rel(config.Root, path)

		if relErr != nil {
			return relErr
		}

		relPath = filepath.ToSlash(relPath)

		// Always allow the walk to start at the root.
		if relPath != "." {
			if matcher.Matches(relPath, entry.IsDir()) {
				if entry.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}
		}

		if entry.IsDir() {
			return nil
		}

		if !indexableExts[filepath.Ext(path)] {
			return nil
		}

		stat, statErr := entry.Info()

		if statErr != nil {
			return fmt.Errorf("reindex: stat %s: %w", path, statErr)
		}

		// Incremental skip: a live file whose mtime AND size are unchanged since
		// the last pass keeps the same content hash and index rows, so there is
		// nothing to re-read, re-hash, or re-enqueue. Stamp the current
		// generation (so the orphan reaper, which reaps rows with an older
		// last_seen_gen, leaves it alone) and move on. Force bypasses this. The
		// skip trades correctness-under-clock-games for speed: a content change
		// that preserves both mtime and size would be missed — use --force then.
		if !config.Force {
			if existing, getErr := config.FileStates.Get(relPath); getErr == nil &&
				existing.State == index.FileStateLive &&
				existing.MtimeNs == stat.ModTime().UnixNano() &&
				existing.Size == stat.Size() {
				if upsertErr := config.FileStates.Upsert(index.FileStateRow{
					Path:        relPath,
					ContentHash: existing.ContentHash,
					MtimeNs:     existing.MtimeNs,
					Size:        existing.Size,
					State:       index.FileStateLive,
					LastSeenGen: gen,
				}); upsertErr != nil {
					return upsertErr
				}

				return nil
			}
		}

		content, readErr := os.ReadFile(path)

		if readErr != nil {
			return fmt.Errorf("reindex: read %s: %w", path, readErr)
		}

		checksum := sha256.Sum256(content)

		if upsertErr := config.FileStates.Upsert(index.FileStateRow{
			Path:        relPath,
			ContentHash: hex.EncodeToString(checksum[:]),
			MtimeNs:     stat.ModTime().UnixNano(),
			Size:        stat.Size(),
			State:       index.FileStateLive,
			LastSeenGen: gen,
		}); upsertErr != nil {
			return upsertErr
		}

		if enqErr := config.EmbedQueue.EnqueueReindex(relPath); enqErr != nil {
			return enqErr
		}

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("reindex: walk: %w", walkErr)
	}

	candidates, listErr := config.FileStates.ListByGenLessThan(gen)

	if listErr != nil {
		return nil, fmt.Errorf("reindex: list orphan candidates: %w", listErr)
	}

	for _, candidate := range candidates {
		// release returns the candidate's lease. Defined per-iteration so it
		// captures the current candidate; factors the ReleaseContext literal
		// repeated across every exit path below.
		release := func() error {
			return config.FileStates.Release(index.ReleaseContext{Path: candidate.Path, WorkerID: workerID})
		}

		_, claimErr := config.FileStates.Claim(candidate.Path, workerID, leaseTTL)

		if errors.Is(claimErr, index.ErrBusy) {
			continue
		}

		if claimErr != nil {
			return nil, fmt.Errorf("reindex: claim %s: %w", candidate.Path, claimErr)
		}

		// Re-check state under the lease: another concurrent walker
		// may have tombstoned this row between our ListByGenLessThan
		// snapshot and our Claim. Skip already-tombstoned rows so we
		// neither double-tombstone nor double-count Removed.
		current, getErr := config.FileStates.Get(candidate.Path)

		if getErr != nil {
			_ = release()
			return nil, fmt.Errorf("reindex: re-read %s: %w", candidate.Path, getErr)
		}

		if current.State != index.FileStateLive {
			if releaseErr := release(); releaseErr != nil {
				return nil, fmt.Errorf("reindex: release %s: %w", candidate.Path, releaseErr)
			}

			continue
		}

		if current.LastSeenGen >= gen {
			// Another walker (or a live writer) already stamped this
			// row with a current-or-future gen. Leave it alone.
			if releaseErr := release(); releaseErr != nil {
				return nil, fmt.Errorf("reindex: release %s: %w", candidate.Path, releaseErr)
			}

			continue
		}

		statPath := filepath.Join(config.Root, candidate.Path)
		_, statErr := os.Stat(statPath)

		switch {
		case statErr == nil:
			// File still exists — another process recreated it between
			// the previous walk and this one (or this walk simply
			// skipped it via the ignore matcher). Stamp last_seen_gen
			// so the next reap doesn't reconsider it, then release.
			if upsertErr := config.FileStates.Upsert(index.FileStateRow{
				Path:        candidate.Path,
				ContentHash: candidate.ContentHash,
				MtimeNs:     candidate.MtimeNs,
				Size:        candidate.Size,
				State:       index.FileStateLive,
				LastSeenGen: gen,
			}); upsertErr != nil {
				_ = release()
				return nil, fmt.Errorf("reindex: refresh gen %s: %w", candidate.Path, upsertErr)
			}

			if releaseErr := release(); releaseErr != nil {
				return nil, fmt.Errorf("reindex: release %s: %w", candidate.Path, releaseErr)
			}

		case errors.Is(statErr, fs.ErrNotExist):
			if tombstoneErr := config.FileStates.Tombstone(candidate.Path); tombstoneErr != nil {
				_ = release()
				return nil, fmt.Errorf("reindex: tombstone %s: %w", candidate.Path, tombstoneErr)
			}

			nodeID := nodeIDForPath(candidate.Path)

			if deleteErr := config.Repo.DeleteByPath(candidate.Path); deleteErr != nil {
				_ = release()
				return nil, fmt.Errorf("reindex: delete node %s: %w", candidate.Path, deleteErr)
			}

			if config.Edges != nil {
				if deleteErr := config.Edges.DeleteBySource(nodeID); deleteErr != nil {
					_ = release()
					return nil, fmt.Errorf("reindex: delete edges %s: %w", nodeID, deleteErr)
				}
			}

			if releaseErr := release(); releaseErr != nil {
				return nil, fmt.Errorf("reindex: release %s: %w", candidate.Path, releaseErr)
			}

			report.Removed++

		default:
			_ = release()
			return nil, fmt.Errorf("reindex: stat %s: %w", statPath, statErr)
		}
	}

	if !config.Async {
		if config.Workers <= 0 {
			if config.Logger != nil {
				config.Logger.Info("reindex: workers=0; sync drain skipped, queue retained",
					"root", config.Root,
					"generation", gen,
				)
			}
		}

		drainReport, drainErr := DrainReindexQueue(context.Background(), WorkerConfig{
			Root:          config.Root,
			Repo:          config.Repo,
			Edges:         config.Edges,
			EdgeTypes:     config.EdgeTypes,
			EmbedQueue:    config.EmbedQueue,
			FileStates:    config.FileStates,
			Manifest:      config.Manifest,
			Behaviors:     config.Behaviors,
			DriftLog:      config.DriftLog,
			NodeTypes:     config.NodeTypes,
			PropertyDrift: config.PropertyDrift,
			Logger:        config.Logger,
			Workers:       config.Workers,
			TTL:           leaseTTL,
			WorkerID:      workerID,
			Generation:    gen,
		})

		if drainErr != nil {
			return nil, fmt.Errorf("reindex: drain reindex queue: %w", drainErr)
		}

		report.mergeDrain(drainReport)
	}

	if config.Embedder != nil && config.Workers > 0 {
		if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:             config.Root,
			Nodes:            config.Repo,
			Queue:            config.EmbedQueue,
			Embeddings:       config.EmbeddingRepo,
			Embedder:         config.Embedder,
			Chunker:          config.Chunker,
			Workers:          config.Workers,
			EmbedConcurrency: config.Workers,
			TTL:              leaseTTL,
			Logger:           config.Logger,
		}); drainErr != nil {
			return nil, drainErr
		}
	}

	if setErr := config.Meta.Set("last_reindex_at", fmt.Sprintf("%d", time.Now().UnixNano())); setErr != nil {
		return nil, fmt.Errorf("reindex: record last_reindex_at: %w", setErr)
	}

	if setErr := config.Meta.Set(edgeDerivationVersionKey, edgeDerivationVersion); setErr != nil {
		return nil, fmt.Errorf("reindex: record %s: %w", edgeDerivationVersionKey, setErr)
	}

	if config.Logger != nil {
		config.Logger.Info("reindex walk complete",
			"root", config.Root,
			"indexed", report.Indexed,
			"removed", report.Removed,
			"skipped", report.Skipped,
			"workflow_violations", report.WorkflowViolations,
			"property_violations", report.PropertyViolations,
			"ref_dangling", report.RefDangling,
			"ref_ambiguous", report.RefAmbiguous,
			"ref_type_mismatch", report.RefTypeMismatch,
			"ref_cycle", report.RefCycle,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}

	return report, nil
}

// appendUnique appends value to slice only if not already present.
func appendUnique(slice []string, value string) []string {
	for _, existing := range slice {
		if existing == value {
			return slice
		}
	}

	return append(slice, value)
}

func instanceFromQualifier(qualified string) string {
	if _, instance, found := strings.Cut(qualified, "."); found {
		return instance
	}

	return qualified
}

func kindFromQualifier(qualified string) string {
	kind, _, _ := strings.Cut(qualified, ".")

	return kind
}

// readStatusFromParsed reads the "status" property as a string. The
// rejection-path drift row uses this since the rejection error doesn't
// carry the status-property name explicitly. (For v1 with one workflow
// configuration per workspace, "status" is the default; non-default
// status-property values surface in the recovery path which carries
// the property explicitly.)
func readStatusFromParsed(parsed *node.Node) string {
	if parsed == nil || parsed.Properties == nil {
		return ""
	}

	value, found := parsed.Properties["status"]

	if !found {
		return ""
	}

	stringValue, ok := value.(string)

	if !ok {
		return ""
	}

	return stringValue
}

// extractPropertyFromError pulls Property off a *workflow.Error if the
// rejection came from the workflow pack; otherwise returns "status".
func extractPropertyFromError(err error) string {
	var workflowErr *workflow.Error

	if errors.As(err, &workflowErr) {
		return workflowErr.Property
	}

	return "status"
}

// workflowErrorCode pulls the rejection Code off a *workflow.Error, or ""
// when the error did not originate from the workflow pack.
func workflowErrorCode(err error) string {
	var workflowErr *workflow.Error

	if errors.As(err, &workflowErr) {
		return string(workflowErr.Code)
	}

	return ""
}

// propertyErrorKindString maps a node.PropertyErrorKind to the string used
// in property_drift rows. Returns "" for kinds that cannot arise from reindex.
func propertyErrorKindString(kind node.PropertyErrorKind) string {
	switch kind {
	case node.ErrTypeMismatch:
		return "type-mismatch"
	case node.ErrRequiredMissing:
		return "required-missing"
	case node.ErrEnumViolation:
		return "enum-violation"
	default:
		// ErrCannotUnsetRequired cannot fire from reindex (no before state).
		return ""
	}
}

// flattenEdges turns parsed.Edges into the EdgeRow shape expected by EdgeRepo.
//
// Each row is tagged with `kind`:
//   - "derived" — the edge-type name matches a ref-property declared on
//     parsedNode.Type (i.e. it was synthesized by `synthesizeRefEdgeTypes`).
//   - "direct"  — every other edge (frontmatter-direct, wikilink-resolved,
//     etc.). Phase 3 Task 3 will refine the direct/wikilink split; this task
//     uses "direct" as the placeholder for everything non-derived.
//
// Source is left NULL: Phase 3 only writes a non-NULL source for structural
// sub-unit edges, which never come through this code path.
func flattenEdges(parsedNode *node.Node, nodeTypes map[string]manifest.NodeType) []index.EdgeRow {
	refProps := refPropertyNamesForType(parsedNode.Type, nodeTypes)

	var rows []index.EdgeRow

	for edgeType, targets := range parsedNode.Edges {
		kind := "direct"

		if _, isRef := refProps[edgeType]; isRef {
			kind = "derived"
		}

		for _, target := range targets {
			rows = append(rows, index.EdgeRow{
				Type:       edgeType,
				SourceID:   parsedNode.ID,
				TargetID:   target,
				SourcePath: parsedNode.Path,
				Kind:       kind,
			})
		}
	}

	return rows
}

// refPropertyNamesForType returns the set of property names that are
// ref-shaped on the given node type. Returns an empty (non-nil) map when the
// type is unknown or has no ref properties.
func refPropertyNamesForType(typeName string, nodeTypes map[string]manifest.NodeType) map[string]struct{} {
	out := map[string]struct{}{}

	nodeType, declared := nodeTypes[typeName]

	if !declared {
		return out
	}

	for _, prop := range nodeType.Properties {
		if manifest.IsRefProperty(prop) {
			out[prop.Name] = struct{}{}
		}
	}

	return out
}
