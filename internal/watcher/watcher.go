package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/germanamz/tusk/internal/ignore"
)

// Watcher wraps fsnotify and emits debounced WatchEvents.
type Watcher struct {
	root      string
	fsWatcher *fsnotify.Watcher
	matcher   ignore.Matcher
}

// New constructs a Watcher rooted at workspaceRoot. The caller must invoke Run
// to start receiving events and Close when done. New also walks the workspace
// once and adds every directory to the watch set so subdirectory changes are
// observed (fsnotify on Linux watches a single directory per Add call).
//
// matcher mirrors the reindex walker's ignore rules: dirs it rejects (.tusk/,
// .git/, [workspace] ignore) are neither watched nor descended into, and Run
// drops any event whose path it rejects. This prevents index writes under
// .tusk/ from tripping the watcher into a self-sustaining reindex loop. A nil
// matcher disables filtering (every directory is watched).
func New(workspaceRoot string, matcher ignore.Matcher) (*Watcher, error) {
	fsWatcher, newErr := fsnotify.NewWatcher()

	if newErr != nil {
		return nil, fmt.Errorf("watcher: new fsnotify: %w", newErr)
	}

	if addErr := fsWatcher.Add(workspaceRoot); addErr != nil {
		fsWatcher.Close()
		return nil, fmt.Errorf("watcher: add %s: %w", workspaceRoot, addErr)
	}

	walkErr := filepath.WalkDir(workspaceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !entry.IsDir() || path == workspaceRoot {
			return nil
		}

		// Mirror the reindex walker: never watch a dir the ignore matcher rejects
		// (.tusk/, .git/, [workspace] ignore). Watching .tusk/ would make every
		// index write trip the watcher into a self-sustaining reindex loop.
		if matcher != nil {
			relPath, relErr := filepath.Rel(workspaceRoot, path)

			if relErr == nil && matcher.Matches(filepath.ToSlash(relPath), true) {
				return filepath.SkipDir
			}
		}

		_ = fsWatcher.Add(path)

		return nil
	})

	if walkErr != nil {
		fsWatcher.Close()
		return nil, fmt.Errorf("watcher: walk: %w", walkErr)
	}

	return &Watcher{root: workspaceRoot, fsWatcher: fsWatcher, matcher: matcher}, nil
}

// Close releases the underlying fsnotify resources.
func (instance *Watcher) Close() error {
	return instance.fsWatcher.Close()
}

// Run blocks until ctx is cancelled, dispatching debounced events to handler.
// The debounce window coalesces rapid-succession events on the same path into
// a single delayed dispatch.
func (instance *Watcher) Run(ctx context.Context, handler EventHandler) error {
	const debounceWindow = 500 * time.Millisecond

	var (
		mu      sync.Mutex
		pending = map[string]*time.Timer{}
	)

	dispatch := func(event WatchEvent) {
		mu.Lock()
		delete(pending, event.Path)
		mu.Unlock()

		_ = handler(event)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case raw, ok := <-instance.fsWatcher.Events:
			if !ok {
				return nil
			}

			relPath, relErr := filepath.Rel(instance.root, raw.Name)

			if relErr != nil {
				continue
			}

			relPath = filepath.ToSlash(relPath)

			// Drop ignored paths even if they reach us via the root-level watch
			// (fsWatcher.Add(workspaceRoot) still reports .tusk/.git children that
			// New's WalkDir skip declined to Add). The event carries no reliable
			// is-dir bit, so probe both interpretations: a bare ignored dir like
			// ".tusk" only matches as a dir, while ".tusk/index.db-wal" matches as
			// a file. Closes the reindex-loop gap.
			if instance.matcher != nil &&
				(instance.matcher.Matches(relPath, false) || instance.matcher.Matches(relPath, true)) {
				continue
			}

			kind := classify(raw.Op)

			scheduled := WatchEvent{Kind: kind, Path: relPath}

			mu.Lock()
			if existing, alreadyPending := pending[relPath]; alreadyPending {
				existing.Stop()
			}

			pending[relPath] = time.AfterFunc(debounceWindow, func() {
				dispatch(scheduled)
			})
			mu.Unlock()
		case watchErr, ok := <-instance.fsWatcher.Errors:
			if !ok {
				return nil
			}

			return fmt.Errorf("watcher: fsnotify: %w", watchErr)
		}
	}
}

func classify(op fsnotify.Op) EventKind {
	switch {
	case op&fsnotify.Create != 0:
		return EventCreate
	case op&fsnotify.Remove != 0:
		return EventDelete
	case op&fsnotify.Rename != 0:
		return EventRename
	}

	return EventModify
}
