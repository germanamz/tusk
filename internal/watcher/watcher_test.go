package watcher_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/ignore"
	"github.com/germanamz/tusk/internal/watcher"
)

func newMatcher(test *testing.T, root string) ignore.Matcher {
	test.Helper()

	matcher, matcherErr := ignore.NewMatcher(root, nil)

	if matcherErr != nil {
		test.Fatalf("NewMatcher: %v", matcherErr)
	}

	return matcher
}

func TestWatcher_EmitsCreateAndModify(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root))

	if newErr != nil {
		test.Fatalf("New: %v", newErr)
	}

	defer watcherInstance.Close()

	var (
		mu     sync.Mutex
		events []watcher.WatchEvent
	)

	handler := func(event watcher.WatchEvent) error {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		_ = watcherInstance.Run(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond) // let watcher start

	target := filepath.Join(root, "new.md")

	if writeErr := os.WriteFile(target, []byte("hello"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	time.Sleep(700 * time.Millisecond) // > debounce window

	mu.Lock()

	if len(events) == 0 {
		mu.Unlock()
		test.Fatalf("expected at least one event")
	}

	hasCreate := false

	for _, evt := range events {
		if evt.Kind == watcher.EventCreate || evt.Kind == watcher.EventModify {
			hasCreate = true
		}
	}

	mu.Unlock()

	if !hasCreate {
		test.Errorf("expected create/modify event for new.md, got %+v", events)
	}
}

// TestWatcher_SkipsIgnoredPaths pins the fix for the runaway reindex loop: the
// watcher must NOT deliver events for paths the ignore matcher rejects (e.g.
// .tusk/, .git/), otherwise an index write trips the watcher → reindex → index
// write → … forever. A normal markdown write must still be delivered.
func TestWatcher_SkipsIgnoredPaths(test *testing.T) {
	root := test.TempDir()

	tuskDir := filepath.Join(root, ".tusk")

	if mkErr := os.MkdirAll(tuskDir, 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root))

	if newErr != nil {
		test.Fatalf("New: %v", newErr)
	}

	defer watcherInstance.Close()

	var (
		mu     sync.Mutex
		events []watcher.WatchEvent
	)

	handler := func(event watcher.WatchEvent) error {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		_ = watcherInstance.Run(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond) // let watcher start

	// Simulate an index write inside the gitignored .tusk/ dir (the loop trigger).
	if writeErr := os.WriteFile(filepath.Join(tuskDir, "index.db"), []byte("x"), 0o644); writeErr != nil {
		test.Fatalf("write index.db: %v", writeErr)
	}

	// A real markdown edit must still flow — this also serves as the sync barrier.
	if writeErr := os.WriteFile(filepath.Join(root, "notes.md"), []byte("hi"), 0o644); writeErr != nil {
		test.Fatalf("write notes.md: %v", writeErr)
	}

	time.Sleep(700 * time.Millisecond) // > debounce window

	mu.Lock()
	defer mu.Unlock()

	sawNotes := false

	for _, evt := range events {
		if strings.HasPrefix(evt.Path, ".tusk") {
			test.Errorf("watcher delivered ignored path %q (would self-trigger reindex)", evt.Path)
		}

		if evt.Path == "notes.md" {
			sawNotes = true
		}
	}

	if !sawNotes {
		test.Errorf("expected event for notes.md, got %+v", events)
	}
}

// TestWatcher_SkipsIgnoredDirCreatedAfterBoot covers the second filter layer:
// an ignored dir that did NOT exist when New walked the tree (so it was never
// SkipDir'd) is created later and surfaces as a child-of-root event through the
// root-level watch. The Run loop must still drop it.
func TestWatcher_SkipsIgnoredDirCreatedAfterBoot(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root))

	if newErr != nil {
		test.Fatalf("New: %v", newErr)
	}

	defer watcherInstance.Close()

	var (
		mu     sync.Mutex
		events []watcher.WatchEvent
	)

	handler := func(event watcher.WatchEvent) error {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		_ = watcherInstance.Run(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond) // let watcher start

	// .git did not exist at boot, so it surfaces as a fresh child-of-root event.
	if mkErr := os.MkdirAll(filepath.Join(root, ".git"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .git: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "real.md"), []byte("hi"), 0o644); writeErr != nil {
		test.Fatalf("write real.md: %v", writeErr)
	}

	time.Sleep(700 * time.Millisecond) // > debounce window

	mu.Lock()
	defer mu.Unlock()

	sawReal := false

	for _, evt := range events {
		if strings.HasPrefix(evt.Path, ".git") {
			test.Errorf("watcher delivered ignored dir event %q", evt.Path)
		}

		if evt.Path == "real.md" {
			sawReal = true
		}
	}

	if !sawReal {
		test.Errorf("expected event for real.md, got %+v", events)
	}
}
