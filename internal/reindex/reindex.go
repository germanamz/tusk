// Package reindex walks a workspace, parses every markdown node, and brings the
// index up to date with what is on disk.
package reindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/germanamz/tusk/internal/subunit"
)

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

	// Meta is optional; when set, Run records `last_reindex_at` (unix nanoseconds
	// formatted as decimal string) at the end of every successful pass.
	Meta *index.MetaRepo

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
}

// Run walks Root, parses every *.md file with valid frontmatter, and upserts
// or removes index rows so the index matches what is on disk. When Edges and
// EdgeTypes are configured, edges are written and removed alongside nodes.
func Run(config Config) (*Report, error) {
	report := &Report{}
	seenPaths := map[string]struct{}{}

	start := time.Now()

	if config.Logger != nil {
		config.Logger.Info("reindex walk start",
			"root", config.Root,
			"ignore_patterns_count", len(config.WorkspaceIgnore),
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

		if filepath.Ext(path) != ".md" {
			return nil
		}

		content, readErr := os.ReadFile(path)

		if readErr != nil {
			return fmt.Errorf("reindex: read %s: %w", path, readErr)
		}

		parsed, parseErr := node.ParseFile(relPath, content)

		if parseErr != nil {
			report.Skipped++

			return nil
		}

		if resolveErr := node.ResolveEdges(parsed, config.EdgeTypes); resolveErr != nil {
			report.Skipped++

			return nil
		}

		node.MaterializeWikilinks(parsed, config.EdgeTypes)

		stat, statErr := entry.Info()

		if statErr != nil {
			return fmt.Errorf("reindex: stat %s: %w", path, statErr)
		}

		propertiesJSON, marshalErr := json.Marshal(parsed.Properties)

		if marshalErr != nil {
			return fmt.Errorf("reindex: marshal %s: %w", relPath, marshalErr)
		}

		checksum := sha256.Sum256(content)

		fileRow := index.NodeRow{
			ID:             parsed.ID,
			Type:           parsed.Type,
			Path:           parsed.Path,
			Title:          parsed.Title,
			PropertiesJSON: string(propertiesJSON),
			LastMtime:      stat.ModTime().UnixNano(),
			LastSize:       stat.Size(),
			LastChecksum:   hex.EncodeToString(checksum[:]),
		}

		if upsertErr := config.Repo.Upsert(fileRow); upsertErr != nil {
			return upsertErr
		}

		// + Plan 7: workflow validation in warn mode. Rejections become
		// drift rows; recoveries become drift rows; clean passes clear
		// any prior drift for this node.
		if config.Behaviors != nil {
			result, fireErr := config.Behaviors.FireNodeWriteValidateWithRecovery(nil, parsed)

			now := time.Now().UnixNano()

			switch {
			case fireErr != nil:
				report.WorkflowViolations++

				if config.DriftLog != nil {
					_ = config.DriftLog.Append(index.WorkflowDriftRow{
						NodeID:         parsed.ID,
						PackInstance:   instanceFromQualifier(result.Rejector),
						PackKind:       kindFromQualifier(result.Rejector),
						ObservedStatus: readStatusFromParsed(parsed),
						Property:       extractPropertyFromError(fireErr),
						ObservedAt:     now,
					})
				}

			case len(result.Recovered) > 0:
				report.WorkflowViolations += len(result.Recovered)

				if config.DriftLog != nil {
					for _, recovered := range result.Recovered {
						_ = config.DriftLog.Append(index.WorkflowDriftRow{
							NodeID:         parsed.ID,
							PackInstance:   recovered.PackInstance,
							PackKind:       recovered.PackKind,
							ObservedStatus: recovered.From,
							Property:       recovered.Property,
							ObservedAt:     now,
						})
					}
				}

			default:
				if config.DriftLog != nil {
					_ = config.DriftLog.ClearForNode(parsed.ID)
				}
			}
		}

		// + Plan 7.b: property validation in warn mode. Hard errors and
		// drift entries both become drift rows; indexing never aborts.
		if config.NodeTypes != nil {
			if _, typed := config.NodeTypes[parsed.Type]; typed {
				propResult := node.ValidateProperties(parsed, config.NodeTypes)

				if config.Behaviors != nil {
					propResult.Drift = node.FilterReservedDrift(propResult.Drift, parsed.Type, config.Behaviors.ReservedProperties())
				}

				now := time.Now().UnixNano()

				for _, hardErr := range propResult.HardErrors {
					report.PropertyViolations++

					if config.PropertyDrift != nil {
						kind := propertyErrorKindString(hardErr.Kind)

						if kind != "" {
							_ = config.PropertyDrift.Append(index.PropertyDriftRow{
								NodeID:     parsed.ID,
								NodeType:   parsed.Type,
								Kind:       kind,
								Property:   hardErr.Property,
								Details:    hardErr.Reason,
								ObservedAt: now,
							})
						}
					}
				}

				for _, drift := range propResult.Drift {
					report.PropertyViolations++

					if config.PropertyDrift != nil {
						_ = config.PropertyDrift.Append(index.PropertyDriftRow{
							NodeID:     parsed.ID,
							NodeType:   parsed.Type,
							Kind:       "undeclared-property",
							Property:   drift.Property,
							Details:    drift.Reason,
							ObservedAt: now,
						})
					}
				}

				if len(propResult.HardErrors) == 0 && len(propResult.Drift) == 0 {
					if config.PropertyDrift != nil {
						_ = config.PropertyDrift.ClearForNode(parsed.ID)
					}
				}

				// + Plan 7.c.1: ref resolution in warn mode. Ref errors become
				// drift rows; clean pass relies on the ClearForNode above (single
				// point per node). Resolved edges are merged into parsed.Edges so
				// the existing edge-write path persists them. Unresolved ref edges
				// are cleared from parsed.Edges so bad values are never stored.
				if config.PropertyDrift != nil {
					refLookup := node.NewIndexRefLookup(config.Repo)
					refResult := node.ResolveRefs(parsed, config.NodeTypes, refLookup)
					refNow := time.Now().UnixNano()

					// Track which ref properties had errors so their edges can be cleared.
					refErrorProps := map[string]struct{}{}

					for _, refErr := range refResult.HardErrors {
						details, _ := json.Marshal(map[string]any{
							"value":       refErr.Value,
							"to":          refErr.To,
							"candidates":  refErr.Candidates,
							"actual_type": refErr.ActualType,
						})

						_ = config.PropertyDrift.Append(index.PropertyDriftRow{
							NodeID:     parsed.ID,
							NodeType:   parsed.Type,
							Kind:       string(refErr.Kind),
							Property:   refErr.Property,
							Details:    string(details),
							ObservedAt: refNow,
						})

						switch refErr.Kind {
						case node.RefErrDangling:
							report.RefDangling++
						case node.RefErrAmbiguous:
							report.RefAmbiguous++
						case node.RefErrTypeMismatch:
							report.RefTypeMismatch++
						case node.RefErrCycle:
							report.RefCycle++
						}

						refErrorProps[refErr.Property] = struct{}{}
					}

					// Clear unresolved ref edges so raw unresolved values are not stored.
					for propName := range refErrorProps {
						delete(parsed.Edges, propName)
					}

					// Merge resolved ref edges: replace raw values with resolved node IDs.
					resolvedByProp := map[string][]string{}

					for _, edge := range refResult.Edges {
						resolvedByProp[edge.EdgeType] = appendUnique(resolvedByProp[edge.EdgeType], edge.TargetID)
					}

					for propName, targets := range resolvedByProp {
						parsed.Edges[propName] = targets
					}
				}
			}
		}

		if config.Edges != nil {
			edgeRows := flattenEdges(parsed, config.NodeTypes)

			if upsertErr := config.Edges.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
				return upsertErr
			}
		}

		// + Plan 2 Task 3: sub-unit diff / insert / delete. The
		// pipeline runs only when the workspace opts in AND the
		// reindex caller supplied a manifest; the existing test
		// suite builds Config without one so the older tests stay
		// on the legacy code path.
		if config.Manifest != nil && config.Manifest.SubUnitsEnabled() && config.Edges != nil {
			units, parseUnitsErr := subunit.Parse(parsed.Body)

			if parseUnitsErr != nil {
				report.Skipped++
				return nil
			}

			sync := &subunit.Sync{
				Repo:     config.Repo,
				EdgeRepo: config.Edges,
				EmbedQ:   config.EmbedQueue,
				Manifest: config.Manifest,
				Logger:   config.Logger,
			}

			syncResult, syncErr := sync.ApplyFile(context.Background(), fileRow, units)

			if syncErr != nil {
				return syncErr
			}

			report.SubUnitsInserted += syncResult.Inserted
			report.SubUnitsDeleted += syncResult.Deleted
			report.SubUnitsReordered += syncResult.Reordered
		}

		seenPaths[parsed.Path] = struct{}{}
		report.Indexed++

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("reindex: walk: %w", walkErr)
	}

	existingRows, listErr := config.Repo.List(index.ListFilter{})

	if listErr != nil {
		return nil, listErr
	}

	for _, row := range existingRows {
		if _, kept := seenPaths[row.Path]; kept {
			continue
		}

		if deleteErr := config.Repo.DeleteByPath(row.Path); deleteErr != nil {
			return nil, deleteErr
		}

		if config.Edges != nil {
			if deleteErr := config.Edges.DeleteBySource(row.ID); deleteErr != nil {
				return nil, deleteErr
			}
		}

		report.Removed++
	}

	if config.Embedder != nil {
		// Enqueue every indexed node so the drain loop covers them.
		for path := range seenPaths {
			id := strings.TrimSuffix(path, ".md")
			_ = config.EmbedQueue.Enqueue(id)
		}

		var manifestTTL int

		if config.Manifest != nil {
			manifestTTL = config.Manifest.Lease.TTLSeconds
		}

		if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:       config.Root,
			Nodes:      config.Repo,
			Queue:      config.EmbedQueue,
			Embeddings: config.EmbeddingRepo,
			Embedder:   config.Embedder,
			Chunker:    config.Chunker,
			Workers:    config.Workers,
			TTL:        leaseconfig.Resolve(manifestTTL),
			Logger:     config.Logger,
		}); drainErr != nil {
			return nil, drainErr
		}
	}

	if config.Meta != nil {
		if setErr := config.Meta.Set("last_reindex_at", fmt.Sprintf("%d", time.Now().UnixNano())); setErr != nil {
			return nil, fmt.Errorf("reindex: record last_reindex_at: %w", setErr)
		}
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
	for index := 0; index < len(qualified); index++ {
		if qualified[index] == '.' {
			return qualified[index+1:]
		}
	}

	return qualified
}

func kindFromQualifier(qualified string) string {
	for index := 0; index < len(qualified); index++ {
		if qualified[index] == '.' {
			return qualified[:index]
		}
	}

	return qualified
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
