package reindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/germanamz/tusk/internal/htmlunit"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/subunit"
	"github.com/germanamz/tusk/internal/typepacks/html"
)

// defaultReindexBatch is the default claim size per Drain call. Workers drain
// in cycles; small enough that lease churn is acceptable, large enough that
// most workspaces drain in 1-2 batches per worker.
const defaultReindexBatch = 32

// MaxReindexAttempts caps how many times a single reindex row is re-leased
// before being dropped from the queue. Mirrors embed.MaxEmbedAttempts.
const MaxReindexAttempts = 3

// WorkerConfig configures DrainReindexQueue. All fields except Workers,
// BatchSize, and Logger are required for a non-trivial workspace.
type WorkerConfig struct {
	Root          string
	Repo          *index.NodeRepo
	Edges         *index.EdgeRepo
	EdgeTypes     manifest.EdgeTypes
	EmbedQueue    *index.EmbedQueueRepo
	FileStates    *index.FileStateRepo
	Manifest      *manifest.Manifest
	Behaviors     node.Behaviors
	DriftLog      *index.WorkflowDriftRepo
	NodeTypes     map[string]manifest.NodeType
	PropertyDrift *index.PropertyDriftRepo
	Logger        *slog.Logger

	// Workers caps the number of goroutines draining the queue in parallel.
	// The caller resolves the final value via embedconfig.ResolveWorkers;
	// 0 means "opt out" — DrainReindexQueue returns immediately without
	// spawning workers or touching the queue.
	Workers int

	// BatchSize is the claim size per Drain call. Zero defaults to defaultReindexBatch.
	BatchSize int

	// TTL is the lease window applied per claim. Zero or negative defaults
	// to leaseconfig-resolved value at the caller; pass through verbatim.
	TTL time.Duration

	// Generation is the current reindex_gen; used to stamp file_state rows
	// after a worker successfully processes a file.
	Generation int64
}

// DrainReport aggregates per-file counters across workers. Mirrors the
// subset of reindex.Report fields the per-file path produces.
type DrainReport struct {
	Indexed            int
	Skipped            int
	WorkflowViolations int
	PropertyViolations int
	RefDangling        int
	RefAmbiguous       int
	RefTypeMismatch    int
	RefCycle           int
	SubUnitsInserted   int
	SubUnitsDeleted    int
	SubUnitsReordered  int
}

func (rep *DrainReport) merge(other DrainReport) {
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

// DrainReindexQueue claims and processes kind='reindex' rows until the queue
// is empty or ctx is cancelled. Returns aggregate per-file counts. Workers
// share the same EmbedQueueRepo; the lease primitive serializes claims.
func DrainReindexQueue(ctx context.Context, cfg WorkerConfig) (DrainReport, error) {
	if cfg.EmbedQueue == nil {
		return DrainReport{}, fmt.Errorf("reindex: drain: EmbedQueue is required")
	}

	if cfg.Repo == nil {
		return DrainReport{}, fmt.Errorf("reindex: drain: Repo is required")
	}

	workers := cfg.Workers

	if workers <= 0 {
		return DrainReport{}, nil
	}

	batch := cfg.BatchSize

	if batch <= 0 {
		batch = defaultReindexBatch
	}

	var (
		mu       sync.Mutex
		report   DrainReport
		firstErr error
	)

	record := func(err error) {
		if err == nil {
			return
		}

		mu.Lock()
		defer mu.Unlock()

		if firstErr == nil {
			firstErr = err
		}
	}

	mergeReport := func(local DrainReport) {
		mu.Lock()
		defer mu.Unlock()
		report.merge(local)
	}

	var waitGroup sync.WaitGroup

	for i := 0; i < workers; i++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			workerID := index.WorkerID()

			var local DrainReport

			for {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return
				}

				rows, drainErr := cfg.EmbedQueue.DrainReindex(workerID, batch, cfg.TTL)

				if drainErr != nil {
					record(drainErr)
					return
				}

				if len(rows) == 0 {
					mergeReport(local)
					return
				}

				for _, row := range rows {
					if ctxErr := ctx.Err(); ctxErr != nil {
						return
					}

					procErr := processReindexJob(cfg, row.NodeID, &local)

					switch {
					case procErr == nil:
						if ackErr := cfg.EmbedQueue.Ack(row.NodeID, workerID); ackErr != nil {
							record(ackErr)
							return
						}

					case errors.Is(procErr, errSkipFile):
						local.Skipped++

						if ackErr := cfg.EmbedQueue.Ack(row.NodeID, workerID); ackErr != nil {
							record(ackErr)
							return
						}

					default:
						nextAttempts := row.Attempts + 1

						if nextAttempts >= MaxReindexAttempts {
							if cfg.Logger != nil {
								cfg.Logger.Warn("reindex gave up",
									"node_id", row.NodeID,
									"attempts", nextAttempts,
									"err", procErr.Error(),
								)
							}

							if dropErr := cfg.EmbedQueue.Drop(row.NodeID, workerID); dropErr != nil {
								record(dropErr)
								return
							}
						} else {
							if cfg.Logger != nil {
								cfg.Logger.Warn("reindex re-enqueued",
									"node_id", row.NodeID,
									"attempts", nextAttempts,
									"err", procErr.Error(),
								)
							}

							if nackErr := cfg.EmbedQueue.Nack(row.NodeID, workerID, procErr); nackErr != nil {
								record(nackErr)
								return
							}
						}
					}
				}
			}
		}()
	}

	waitGroup.Wait()

	return report, firstErr
}

// errSkipFile signals that a reindex job should be acked (work complete from
// the queue's perspective) but counted under Skipped rather than Indexed —
// e.g. unparseable frontmatter, file vanished since enqueue. Processing
// errors propagate as ordinary errors and trigger Nack/Drop.
var errSkipFile = errors.New("reindex: skip file")

// processReindexJob performs the per-file work for one reindex queue row:
// reads the file, parses, upserts node + edges, runs workflow/property/ref
// drift, applies sub-unit sync, and stamps file_state. Mirrors the BRIDGE
// block previously inlined in Run.
func processReindexJob(cfg WorkerConfig, nodeID string, report *DrainReport) error {
	relPath := strings.TrimPrefix(nodeID, index.ReindexNodeIDPrefix)
	absPath := filepath.Join(cfg.Root, relPath)

	content, readErr := os.ReadFile(absPath)

	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return errSkipFile
		}

		return fmt.Errorf("reindex: read %s: %w", relPath, readErr)
	}

	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return errSkipFile
		}

		return fmt.Errorf("reindex: stat %s: %w", relPath, statErr)
	}

	parsed, parseErr := parseContentFile(relPath, content)

	if parseErr != nil {
		return errSkipFile
	}

	if resolveErr := node.ResolveEdges(parsed, cfg.EdgeTypes); resolveErr != nil {
		return errSkipFile
	}

	node.MaterializeWikilinks(parsed, cfg.EdgeTypes)

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

	if upsertErr := cfg.Repo.Upsert(fileRow); upsertErr != nil {
		return upsertErr
	}

	if cfg.Behaviors != nil {
		// Reindex reads already-persisted state; it is not a write event, so
		// there is no transition to validate. Passing the node as its own
		// "before" makes the validator take the no-transition fast-path
		// (before == after), flagging only a status value that is not a
		// declared state at all (genuine on-disk drift). Transition legality
		// and the initial-state-on-create rule are write-time policies enforced
		// at the node create/modify boundary, where a real "before" exists;
		// reindex has no access to a node's prior status and cannot — and must
		// not — re-litigate transition history.
		result, fireErr := cfg.Behaviors.FireNodeWriteValidateWithRecovery(parsed, parsed)

		// With before == after the validator can only reject with a hard error
		// (a status that is not a declared state); orphan-state recovery — which
		// requires a transition out of an unknown prior status — cannot arise on
		// this path. Recovery is surfaced on the write-time modify path instead.
		if fireErr != nil {
			report.WorkflowViolations++

			if cfg.DriftLog != nil {
				_ = cfg.DriftLog.Append(index.WorkflowDriftRow{
					NodeID:         parsed.ID,
					PackInstance:   instanceFromQualifier(result.Rejector),
					PackKind:       kindFromQualifier(result.Rejector),
					ObservedStatus: readStatusFromParsed(parsed),
					Property:       extractPropertyFromError(fireErr),
					ErrorCode:      workflowErrorCode(fireErr),
					Detail:         fireErr.Error(),
					ObservedAt:     time.Now().UnixNano(),
				})
			}
		} else if cfg.DriftLog != nil {
			_ = cfg.DriftLog.ClearForNode(parsed.ID)
		}
	}

	if cfg.NodeTypes != nil {
		if _, typed := cfg.NodeTypes[parsed.Type]; typed {
			propResult := node.ValidateProperties(parsed, cfg.NodeTypes)

			if cfg.Behaviors != nil {
				reserved := cfg.Behaviors.ReservedProperties()

				// HTML nodes carry data-* signals under the reserved
				// node.HTMLSignalsKey; exempt it from drift so signals never
				// surface as undeclared user properties.
				if isHTMLPath(parsed.Path) {
					reserved = htmlReservedDrift(reserved, parsed.Type)
				}

				propResult.Drift = node.FilterReservedDrift(propResult.Drift, parsed.Type, reserved)
			}

			now := time.Now().UnixNano()

			for _, hardErr := range propResult.HardErrors {
				report.PropertyViolations++

				if cfg.PropertyDrift != nil {
					kind := propertyErrorKindString(hardErr.Kind)

					if kind != "" {
						_ = cfg.PropertyDrift.Append(index.PropertyDriftRow{
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

				if cfg.PropertyDrift != nil {
					_ = cfg.PropertyDrift.Append(index.PropertyDriftRow{
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
				if cfg.PropertyDrift != nil {
					_ = cfg.PropertyDrift.ClearForNode(parsed.ID)
				}
			}

			if cfg.PropertyDrift != nil {
				refLookup := node.NewIndexRefLookup(cfg.Repo)
				refResult := node.ResolveRefs(parsed, cfg.NodeTypes, refLookup)
				refNow := time.Now().UnixNano()

				refErrorProps := map[string]struct{}{}

				for _, refErr := range refResult.HardErrors {
					details, _ := json.Marshal(map[string]any{
						"value":       refErr.Value,
						"to":          refErr.To,
						"candidates":  refErr.Candidates,
						"actual_type": refErr.ActualType,
					})

					_ = cfg.PropertyDrift.Append(index.PropertyDriftRow{
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

				for propName := range refErrorProps {
					delete(parsed.Edges, propName)
				}

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

	if cfg.Edges != nil {
		edgeRows := flattenEdges(parsed, cfg.NodeTypes)

		if upsertErr := cfg.Edges.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
			return upsertErr
		}
	}

	if cfg.Manifest != nil && cfg.Manifest.SubUnitsEnabled() && cfg.Edges != nil {
		var (
			units         []subunit.Unit
			parseUnitsErr error
			subSource     string
		)

		switch filepath.Ext(relPath) {
		case ".html", ".htm":
			// htmlunit.Parse needs the raw HTML to recover the heading
			// outline and block structure; parsed.Body is the normalized
			// plaintext (Phase 4), which has no markup left to address.
			units, parseUnitsErr = htmlunit.Parse(content)
			subSource = html.Source()
		default:
			units, parseUnitsErr = subunit.Parse(parsed.Body)
			subSource = "markdown"
		}

		if parseUnitsErr != nil {
			return errSkipFile
		}

		sync := &subunit.Sync{
			Repo:     cfg.Repo,
			EdgeRepo: cfg.Edges,
			EmbedQ:   cfg.EmbedQueue,
			Manifest: cfg.Manifest,
			Logger:   cfg.Logger,
			Source:   subSource,
		}

		syncResult, syncErr := sync.ApplyFile(context.Background(), fileRow, units)

		if syncErr != nil {
			return syncErr
		}

		report.SubUnitsInserted += syncResult.Inserted
		report.SubUnitsDeleted += syncResult.Deleted
		report.SubUnitsReordered += syncResult.Reordered
	}

	if cfg.FileStates != nil {
		if upsertErr := cfg.FileStates.Upsert(index.FileStateRow{
			Path:        parsed.Path,
			ContentHash: hex.EncodeToString(checksum[:]),
			MtimeNs:     stat.ModTime().UnixNano(),
			Size:        stat.Size(),
			State:       index.FileStateLive,
			LastSeenGen: cfg.Generation,
		}); upsertErr != nil {
			return upsertErr
		}
	}

	// Enqueue an embed job for this node. Sub-unit rows enqueue their own
	// embed jobs via subunit.Sync; here we cover the file-level row.
	if enqErr := cfg.EmbedQueue.Enqueue(parsed.ID); enqErr != nil {
		return enqErr
	}

	report.Indexed++

	return nil
}
