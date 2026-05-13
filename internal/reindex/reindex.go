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
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
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

		if _, hasReferences := config.EdgeTypes["references"]; hasReferences {
			for _, target := range node.ExtractWikilinks(parsed.Body) {
				parsed.Edges["references"] = appendUnique(parsed.Edges["references"], target)
			}
		}

		stat, statErr := entry.Info()

		if statErr != nil {
			return fmt.Errorf("reindex: stat %s: %w", path, statErr)
		}

		propertiesJSON, marshalErr := json.Marshal(parsed.Properties)

		if marshalErr != nil {
			return fmt.Errorf("reindex: marshal %s: %w", relPath, marshalErr)
		}

		checksum := sha256.Sum256(content)

		if upsertErr := config.Repo.Upsert(index.NodeRow{
			ID:             parsed.ID,
			Type:           parsed.Type,
			Path:           parsed.Path,
			Title:          parsed.Title,
			PropertiesJSON: string(propertiesJSON),
			LastMtime:      stat.ModTime().UnixNano(),
			LastSize:       stat.Size(),
			LastChecksum:   hex.EncodeToString(checksum[:]),
		}); upsertErr != nil {
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
			edgeRows := flattenEdges(parsed)

			if upsertErr := config.Edges.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
				return upsertErr
			}
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

		if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:       config.Root,
			Nodes:      config.Repo,
			Queue:      config.EmbedQueue,
			Embeddings: config.EmbeddingRepo,
			Embedder:   config.Embedder,
			Chunker:    config.Chunker,
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
			"indexed", report.Indexed,
			"removed", report.Removed,
			"skipped", report.Skipped,
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
func flattenEdges(parsedNode *node.Node) []index.EdgeRow {
	var rows []index.EdgeRow

	for edgeType, targets := range parsedNode.Edges {
		for ordinal, target := range targets {
			rows = append(rows, index.EdgeRow{
				Type:       edgeType,
				SourceID:   parsedNode.ID,
				TargetID:   target,
				Ordinal:    ordinal,
				SourcePath: parsedNode.Path,
			})
		}
	}

	return rows
}
