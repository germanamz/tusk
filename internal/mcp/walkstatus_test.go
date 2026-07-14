package mcp

import (
	"errors"
	"sync"
	"testing"

	"github.com/germanamz/tusk/internal/reindex"
)

func TestWalkStatus_NilIsSafe(test *testing.T) {
	var status *WalkStatus // nil

	status.Begin()
	status.End(&reindex.Report{Indexed: 3}, nil)

	if snap := status.Snapshot(); snap != (WalkStatusSnapshot{}) {
		test.Fatalf("nil WalkStatus Snapshot = %+v, want zero value", snap)
	}
}

func TestWalkStatus_BeginSetsWalking(test *testing.T) {
	status := NewWalkStatus()

	if status.Snapshot().Walking {
		test.Fatal("fresh WalkStatus should not be walking")
	}

	status.Begin()

	snap := status.Snapshot()

	if !snap.Walking {
		test.Error("after Begin, Walking should be true")
	}

	if snap.EverWalked {
		test.Error("EverWalked should stay false until a walk completes")
	}
}

func TestWalkStatus_EndRecordsSummary(test *testing.T) {
	status := NewWalkStatus()

	status.Begin()
	status.End(&reindex.Report{Indexed: 2, Removed: 1, Skipped: 4}, nil)

	snap := status.Snapshot()

	if snap.Walking {
		test.Error("after End, Walking should be false")
	}

	if !snap.EverWalked {
		test.Error("after End, EverWalked should be true")
	}

	if snap.Completed != 1 {
		test.Errorf("Completed = %d, want 1", snap.Completed)
	}

	if snap.Last.Indexed != 2 || snap.Last.Removed != 1 || snap.Last.Skipped != 4 {
		test.Errorf("Last = %+v, want Indexed=2 Removed=1 Skipped=4", snap.Last)
	}

	if got := snap.Last.Changed(); got != 3 {
		test.Errorf("Changed() = %d, want 3 (indexed+removed)", got)
	}
}

func TestWalkStatus_EndRecordsError(test *testing.T) {
	status := NewWalkStatus()

	status.Begin()
	status.End(nil, errors.New("boom"))

	snap := status.Snapshot()

	if snap.Walking {
		test.Error("after a failed walk, Walking should be false")
	}

	if snap.Last.Err != "boom" {
		test.Errorf("Last.Err = %q, want %q", snap.Last.Err, "boom")
	}
}

// TestWalkStatus_ConcurrentAccess pins goroutine safety: CI runs go test -race,
// so a walk-writing goroutine and a console-reading goroutine must never race on
// the tracker's fields.
func TestWalkStatus_ConcurrentAccess(test *testing.T) {
	status := NewWalkStatus()

	var waitGroup sync.WaitGroup

	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()

		for iter := 0; iter < 1000; iter++ {
			status.Begin()
			status.End(&reindex.Report{Indexed: iter}, nil)
		}
	}()

	go func() {
		defer waitGroup.Done()

		for iter := 0; iter < 1000; iter++ {
			_ = status.Snapshot()
		}
	}()

	waitGroup.Wait()

	if snap := status.Snapshot(); snap.Completed != 1000 {
		test.Errorf("Completed = %d, want 1000", snap.Completed)
	}
}
