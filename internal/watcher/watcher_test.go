package watcher_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root), nil)

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

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root), nil)

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

// TestWatcher_DropsNonNodeFileEvents pins the fix for the redirect-into-tree
// rewalk loop: a write to a file whose extension can never be a node (a
// redirected .log, a .txt, an image) must NOT be delivered — otherwise
// `tusk graph -v > graph.log` inside the workspace arms a self-sustaining loop
// (each walk's log line is a MODIFY that schedules the next walk). Node files
// (.md/.html), the manifest (tusk.toml), and directory creates must still flow.
func TestWatcher_DropsNonNodeFileEvents(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root), nil)

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

	// The loop trigger: a log redirected into the watched tree. A .log file can
	// never become a node, so this MODIFY must be dropped.
	if writeErr := os.WriteFile(filepath.Join(root, "graph.log"), []byte("walk complete\n"), 0o644); writeErr != nil {
		test.Fatalf("write graph.log: %v", writeErr)
	}

	// A manifest edit must still schedule a walk.
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[workspace]\nname = \"x\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	// A real markdown edit must still flow — this also serves as the sync barrier.
	if writeErr := os.WriteFile(filepath.Join(root, "notes.md"), []byte("hi"), 0o644); writeErr != nil {
		test.Fatalf("write notes.md: %v", writeErr)
	}

	time.Sleep(700 * time.Millisecond) // > debounce window

	mu.Lock()
	defer mu.Unlock()

	sawNotes, sawManifest := false, false

	for _, evt := range events {
		if evt.Path == "graph.log" {
			test.Errorf("watcher delivered non-node file %q (would self-trigger reindex loop)", evt.Path)
		}

		if evt.Path == "notes.md" {
			sawNotes = true
		}

		if evt.Path == "tusk.toml" {
			sawManifest = true
		}
	}

	if !sawNotes {
		test.Errorf("expected event for notes.md, got %+v", events)
	}

	if !sawManifest {
		test.Errorf("expected event for tusk.toml (manifest edits must reindex), got %+v", events)
	}
}

// TestWatcher_ReindexesOnDottedNameDirCreate pins the fix for a regression the
// extension-only filter would introduce: a directory whose NAME contains a dot
// ("archive.v2", or a hidden ".notes") must still schedule a reindex — its
// create/rename can move a whole subtree of nodes in or out, and that directory
// event is the only signal for pre-existing contents moved in wholesale. An
// extension heuristic reads "archive.v2" as a ".v2" file and drops it; os.Stat
// classifies the directory correctly.
func TestWatcher_ReindexesOnDottedNameDirCreate(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root), nil)

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

	// A directory with a dot in its name — the extension-only trap.
	if mkErr := os.Mkdir(filepath.Join(root, "archive.v2"), 0o755); mkErr != nil {
		test.Fatalf("mkdir archive.v2: %v", mkErr)
	}

	// A real markdown edit as the sync barrier.
	if writeErr := os.WriteFile(filepath.Join(root, "real.md"), []byte("hi"), 0o644); writeErr != nil {
		test.Fatalf("write real.md: %v", writeErr)
	}

	time.Sleep(700 * time.Millisecond) // > debounce window

	mu.Lock()
	defer mu.Unlock()

	sawArchive := false

	for _, evt := range events {
		if evt.Path == "archive.v2" {
			sawArchive = true
		}
	}

	if !sawArchive {
		test.Errorf("expected a reindex-triggering event for the dotted-name dir archive.v2, got %+v", events)
	}
}

// TestWatcher_StopsTimersOnShutdown pins the A3 leak fix: a debounce timer
// scheduled before shutdown must NOT fire afterwards. Before A3 the AfterFunc
// timers were never stopped, so a cancelled watcher could still run a full
// reindex ~500ms after Run returned.
func TestWatcher_StopsTimersOnShutdown(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root), nil)

	if newErr != nil {
		test.Fatalf("New: %v", newErr)
	}

	defer watcherInstance.Close()

	var calls atomic.Int64

	handler := func(event watcher.WatchEvent) error {
		calls.Add(1)

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})

	go func() {
		_ = watcherInstance.Run(ctx, handler)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond) // let watcher start

	// Schedule a 500ms debounce timer, then cancel well before it fires.
	if writeErr := os.WriteFile(filepath.Join(root, "a.md"), []byte("hi"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	time.Sleep(100 * time.Millisecond) // timer pending, not yet fired
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		test.Fatal("Run did not return after cancel")
	}

	time.Sleep(700 * time.Millisecond) // past the debounce window: a leaked timer would fire here

	if got := calls.Load(); got != 0 {
		test.Errorf("handler fired %d times after shutdown; want 0 (timer must be stopped on cancel)", got)
	}
}

// TestWatcher_HandlersRunSerially pins the A3 serialization fix: a burst of N
// file changes must not fan out into N concurrent handler runs (each a
// full-vault reindex under `tusk watch`). The dispatcher runs at most one
// handler at a time.
func TestWatcher_HandlersRunSerially(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root), nil)

	if newErr != nil {
		test.Fatalf("New: %v", newErr)
	}

	defer watcherInstance.Close()

	var (
		inFlight atomic.Int32
		maxSeen  atomic.Int32
		calls    atomic.Int32
	)

	handler := func(event watcher.WatchEvent) error {
		current := inFlight.Add(1)

		for {
			prev := maxSeen.Load()

			if current <= prev || maxSeen.CompareAndSwap(prev, current) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond) // hold long enough to overlap if concurrent
		inFlight.Add(-1)
		calls.Add(1)

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = watcherInstance.Run(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond) // let watcher start

	// Burst of distinct files within one debounce window: their timers all fire
	// at ~the same instant. Without serialization the handlers overlap.
	for fileIdx := 0; fileIdx < 8; fileIdx++ {
		name := filepath.Join(root, fmt.Sprintf("f%d.md", fileIdx))

		if writeErr := os.WriteFile(name, []byte("x"), 0o644); writeErr != nil {
			test.Fatalf("write: %v", writeErr)
		}
	}

	time.Sleep(1500 * time.Millisecond) // debounce + 8 serial 50ms handlers + slack

	if calls.Load() == 0 {
		test.Fatal("expected handler invocations for the burst")
	}

	if got := maxSeen.Load(); got > 1 {
		test.Errorf("max concurrent handlers = %d, want 1 (dispatch must be serialized)", got)
	}
}

// TestWatcher_AddsNewDirsOnCreate pins the A3 new-directory fix: a directory
// created after boot must be added to the watch set, else edits inside it are
// never observed (kqueue/inotify watch a single directory per Add).
func TestWatcher_AddsNewDirsOnCreate(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root), nil)

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = watcherInstance.Run(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond) // let watcher start

	// A subdir that did not exist at boot — so it is not watched until the create
	// handler adds it.
	subDir := filepath.Join(root, "sub")

	if mkErr := os.Mkdir(subDir, 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	time.Sleep(300 * time.Millisecond) // let the create event add the dir to the watch

	// A file inside the new subdir: only observed if the subdir is watched.
	if writeErr := os.WriteFile(filepath.Join(subDir, "inner.md"), []byte("hi"), 0o644); writeErr != nil {
		test.Fatalf("write inner: %v", writeErr)
	}

	time.Sleep(700 * time.Millisecond) // > debounce window

	mu.Lock()
	defer mu.Unlock()

	sawInner := false

	for _, evt := range events {
		if evt.Path == "sub/inner.md" {
			sawInner = true
		}
	}

	if !sawInner {
		test.Errorf("expected event for sub/inner.md (new dir must be watched); got %+v", events)
	}
}

// TestWatcher_SkipsIgnoredDirCreatedAfterBoot covers the second filter layer:
// an ignored dir that did NOT exist when New walked the tree (so it was never
// SkipDir'd) is created later and surfaces as a child-of-root event through the
// root-level watch. The Run loop must still drop it.
func TestWatcher_SkipsIgnoredDirCreatedAfterBoot(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root, newMatcher(test, root), nil)

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
