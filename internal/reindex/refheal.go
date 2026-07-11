package reindex

import (
	"context"
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// HealReport summarizes one ref-drift heal pass. All counts are drift rows
// (one row per node+kind+property), not files.
//
// The Remaining* fields are recounted from the drift table after the heal
// drain rather than taken from the drain's own counters: the queue is shared
// cross-process, so a sibling drainer (a `tusk mcp serve` tick) may claim and
// process the enqueued rows — invisible to this drain's counters but not to
// the table.
type HealReport struct {
	Attempted             int         // ref drift rows found before the heal drain
	Healed                int         // before-rows no longer present (by node+kind+property) after the drain
	RemainingDangling     int         // ref_dangling rows still standing after the heal
	RemainingAmbiguous    int         // ref_ambiguous rows still standing after the heal
	RemainingTypeMismatch int         // ref_type_mismatch rows still standing after the heal
	RemainingCycle        int         // ref_cycle rows still standing after the heal
	Drain                 DrainReport // counters from the rows THIS drain claimed (logging only)
}

// enqueueRefDriftRows re-enqueues the file behind every ref-kind drift row so
// the next drain re-resolves it. Ref errors depend on the state of other
// nodes — a fresh index resolves each file against a partially built node
// set, and a target created later never wakes the (unchanged, walk-skipped)
// files that reference it — so recorded ref drift must be retried whenever
// the node set may have changed, not only when the drifted file itself
// changes.
//
// Rows whose node no longer exists are orphans — drift is only ever written
// while parsing a live file, and neither the reap nor rename clears the old
// node id's rows — so they are deleted here instead of being retried (and
// shown by doctor) forever. Returns the number of files enqueued and the set
// of orphaned node ids whose rows were swept.
func enqueueRefDriftRows(rows []index.PropertyDriftRow, drift *index.PropertyDriftRepo, repo *index.NodeRepo, queue *index.EmbedQueueRepo) (int, map[string]bool, error) {
	enqueued := 0
	orphaned := map[string]bool{}
	seen := map[string]bool{}

	for _, row := range rows {
		if seen[row.NodeID] {
			continue
		}

		seen[row.NodeID] = true

		nodeRow, getErr := repo.Get(row.NodeID)

		if errors.Is(getErr, index.ErrNodeNotFound) {
			_ = drift.ClearForNode(row.NodeID)

			orphaned[row.NodeID] = true

			continue
		}

		if getErr != nil {
			return enqueued, orphaned, fmt.Errorf("reindex: heal: resolve node %s: %w", row.NodeID, getErr)
		}

		if enqErr := queue.EnqueueReindex(nodeRow.Path); enqErr != nil {
			return enqueued, orphaned, fmt.Errorf("reindex: heal: enqueue %s: %w", nodeRow.Path, enqErr)
		}

		enqueued++
	}

	return enqueued, orphaned, nil
}

// sweepOrphanDrift deletes property- and workflow-drift rows whose node no
// longer resolves to a node row. Both repos are optional; a nil repo is
// skipped. Called once per reindex, after the orphan reap and before the
// drain, so a delete/rename that left drift behind — or an out-of-band file
// removal — stops being reported by doctor (#685) instead of lingering until a
// `tusk reset`.
func sweepOrphanDrift(config Config) error {
	if config.PropertyDrift != nil {
		if _, sweepErr := config.PropertyDrift.DeleteOrphans(); sweepErr != nil {
			return fmt.Errorf("reindex: sweep orphan property drift: %w", sweepErr)
		}
	}

	if config.DriftLog != nil {
		if _, sweepErr := config.DriftLog.DeleteOrphans(); sweepErr != nil {
			return fmt.Errorf("reindex: sweep orphan workflow drift: %w", sweepErr)
		}
	}

	return nil
}

// enqueueRefDrift lists the current ref-kind drift rows and enqueues their
// files. The async reindex path uses this to hand retry work to the
// background drainer; the sync path retries inline via HealRefDrift.
func enqueueRefDrift(drift *index.PropertyDriftRepo, repo *index.NodeRepo, queue *index.EmbedQueueRepo) (int, error) {
	rows, listErr := drift.ListRefKinds()

	if listErr != nil {
		return 0, fmt.Errorf("reindex: heal: list ref drift: %w", listErr)
	}

	enqueued, _, enqErr := enqueueRefDriftRows(rows, drift, repo, queue)

	return enqueued, enqErr
}

// driftRowKey identifies a drift row by its primary key for set membership.
func driftRowKey(row index.PropertyDriftRow) string {
	return row.NodeID + "\x00" + row.Kind + "\x00" + row.Property
}

// HealRefDrift retries every recorded ref-drift row by re-enqueueing its file
// and draining the reindex queue once more. Run once after a sweep's drain:
// by then every live file has a node row, so refs that dangled only because
// their target sorted later in the walk (or was created after the referencing
// file was last indexed) resolve, their edges are written, and their drift
// rows clear. Genuinely broken refs simply re-record their drift.
//
// No-ops (Attempted == 0) when the config lacks the drift repo or workers, or
// when there is no ref drift to retry.
func HealRefDrift(ctx context.Context, cfg WorkerConfig) (HealReport, error) {
	if cfg.PropertyDrift == nil || cfg.Repo == nil || cfg.EmbedQueue == nil || cfg.Workers <= 0 {
		return HealReport{}, nil
	}

	rows, listErr := cfg.PropertyDrift.ListRefKinds()

	if listErr != nil {
		return HealReport{}, fmt.Errorf("reindex: heal: list ref drift: %w", listErr)
	}

	if len(rows) == 0 {
		return HealReport{}, nil
	}

	enqueued, orphaned, enqErr := enqueueRefDriftRows(rows, cfg.PropertyDrift, cfg.Repo, cfg.EmbedQueue)

	if enqErr != nil {
		return HealReport{}, enqErr
	}

	var drainReport DrainReport

	if enqueued > 0 {
		var drainErr error

		drainReport, drainErr = DrainReindexQueue(ctx, cfg)

		if drainErr != nil {
			return HealReport{}, fmt.Errorf("reindex: heal: drain: %w", drainErr)
		}
	}

	remaining, recountErr := cfg.PropertyDrift.ListRefKinds()

	if recountErr != nil {
		return HealReport{}, fmt.Errorf("reindex: heal: recount ref drift: %w", recountErr)
	}

	report := HealReport{Attempted: len(rows), Drain: drainReport}

	standing := map[string]bool{}

	for _, row := range remaining {
		standing[driftRowKey(row)] = true

		switch row.Kind {
		case string(node.RefErrDangling):
			report.RemainingDangling++
		case string(node.RefErrAmbiguous):
			report.RemainingAmbiguous++
		case string(node.RefErrTypeMismatch):
			report.RemainingTypeMismatch++
		case string(node.RefErrCycle):
			report.RemainingCycle++
		}
	}

	// Per-row identity, not a net count delta: a heal that coincides with a
	// new drift row appearing (a kind flip, a sibling's write) must still
	// count the rows that actually resolved. Orphan-swept rows were removed,
	// not healed — their node is gone, so they don't count.
	for _, row := range rows {
		if !standing[driftRowKey(row)] && !orphaned[row.NodeID] {
			report.Healed++
		}
	}

	return report, nil
}
