package mcp

import (
	"sync"
	"time"

	"github.com/germanamz/tusk/internal/reindex"
)

// WalkStatus is a goroutine-safe record of reindex-walk activity. It is shared
// across runtime swaps (carried like Logger through installStoreLocked and
// buildReloaded) so a long-running console — `tusk graph`'s status footer —
// can render a live idle/walking indicator and a last-walk summary instead of
// the bare, ever-climbing generation counter, which reads as stuck pending work
// on a quiet workspace even when every walk completes in milliseconds and
// changes nothing.
//
// A nil *WalkStatus is safe: every method no-ops (Snapshot returns the zero
// value), so the walk sites need no nil guards.
type WalkStatus struct {
	mu         sync.Mutex
	walking    bool
	startedAt  time.Time
	completed  int64
	everWalked bool
	lastWalk   WalkSummary
}

// WalkSummary is the outcome of the most recently completed walk.
type WalkSummary struct {
	Indexed    int
	Removed    int
	Skipped    int
	DurationMs int64
	Err        string // non-empty when the walk returned an error
}

// Changed reports how many nodes the walk added or removed — the count that
// distinguishes a no-op walk (0 changed) from real work.
func (summary WalkSummary) Changed() int {
	return summary.Indexed + summary.Removed
}

// WalkStatusSnapshot is an immutable point-in-time view for the console.
type WalkStatusSnapshot struct {
	Walking    bool
	Completed  int64
	EverWalked bool
	Last       WalkSummary
}

// NewWalkStatus returns a ready-to-use tracker.
func NewWalkStatus() *WalkStatus {
	return &WalkStatus{}
}

// Begin marks a walk as in progress and starts its timer.
func (status *WalkStatus) Begin() {
	if status == nil {
		return
	}

	status.mu.Lock()
	status.walking = true
	status.startedAt = time.Now()
	status.mu.Unlock()
}

// End records a completed walk's outcome and clears the in-progress flag. report
// may be nil (the walk errored before producing one); walkErr may be nil (the
// walk succeeded). Duration is measured from the paired Begin.
func (status *WalkStatus) End(report *reindex.Report, walkErr error) {
	if status == nil {
		return
	}

	status.mu.Lock()
	defer status.mu.Unlock()

	summary := WalkSummary{}

	if status.walking {
		summary.DurationMs = time.Since(status.startedAt).Milliseconds()
	}

	if report != nil {
		summary.Indexed = report.Indexed
		summary.Removed = report.Removed
		summary.Skipped = report.Skipped
	}

	if walkErr != nil {
		summary.Err = walkErr.Error()
	}

	status.walking = false
	status.everWalked = true
	status.completed++
	status.lastWalk = summary
}

// Snapshot returns the current state under the lock.
func (status *WalkStatus) Snapshot() WalkStatusSnapshot {
	if status == nil {
		return WalkStatusSnapshot{}
	}

	status.mu.Lock()
	defer status.mu.Unlock()

	return WalkStatusSnapshot{
		Walking:    status.walking,
		Completed:  status.completed,
		EverWalked: status.everWalked,
		Last:       status.lastWalk,
	}
}
