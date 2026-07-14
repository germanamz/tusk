package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/germanamz/tusk/internal/ignore"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/workspace"
)

// maxLoggedWatchDirs caps how many failed-to-watch directory paths the
// aggregated Warn lists, so an inotify-capacity blowout produces one bounded
// log line rather than thousands.
const maxLoggedWatchDirs = 10

// Watcher wraps fsnotify and emits debounced WatchEvents.
type Watcher struct {
	root      string
	fsWatcher *fsnotify.Watcher
	matcher   ignore.Matcher
	logger    *slog.Logger
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
//
// logger (may be nil) receives a single aggregated Warn if any directory could
// not be added to the watch set — most often because the OS inotify/kqueue
// capacity is exhausted, which would otherwise leave subtrees silently
// unwatched.
func New(workspaceRoot string, matcher ignore.Matcher, logger *slog.Logger) (*Watcher, error) {
	fsWatcher, newErr := fsnotify.NewWatcher()

	if newErr != nil {
		return nil, fmt.Errorf("watcher: new fsnotify: %w", newErr)
	}

	if addErr := fsWatcher.Add(workspaceRoot); addErr != nil {
		fsWatcher.Close()
		return nil, fmt.Errorf("watcher: add %s: %w", workspaceRoot, addErr)
	}

	var failedDirs []string

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

		if addErr := fsWatcher.Add(path); addErr != nil {
			failedDirs = append(failedDirs, path)
		}

		return nil
	})

	if walkErr != nil {
		fsWatcher.Close()
		return nil, fmt.Errorf("watcher: walk: %w", walkErr)
	}

	logFailedWatchDirs(logger, failedDirs)

	return &Watcher{root: workspaceRoot, fsWatcher: fsWatcher, matcher: matcher, logger: logger}, nil
}

// logFailedWatchDirs emits one bounded Warn naming the directories that could
// not be added to the watch set. A non-empty list almost always means the OS
// watch-descriptor limit was hit (inotify max_user_watches on Linux), in which
// case those subtrees are unwatched and edits there go unobserved until the
// next manual reindex — silent before this was surfaced.
func logFailedWatchDirs(logger *slog.Logger, failedDirs []string) {
	if logger == nil || len(failedDirs) == 0 {
		return
	}

	sample := failedDirs

	if len(sample) > maxLoggedWatchDirs {
		sample = sample[:maxLoggedWatchDirs]
	}

	logger.Warn("watcher: some directories could not be watched (OS watch limit reached?); edits there will go unobserved until the next reindex",
		"failed", len(failedDirs),
		"sample", strings.Join(sample, ", "),
	)
}

// Close releases the underlying fsnotify resources.
func (instance *Watcher) Close() error {
	return instance.fsWatcher.Close()
}

// Run blocks until ctx is cancelled, dispatching debounced events to handler.
// The debounce window coalesces rapid-succession events on the same path into a
// single delayed dispatch. Handler invocations are serialized through one
// dispatcher goroutine, so a burst of N file changes never fans out into N
// concurrent handler runs (each of which, under `tusk watch`, is a full-vault
// reindex). Every outstanding debounce timer is stopped before Run returns, so
// no timer can fire — and no reindex can start — after shutdown.
func (instance *Watcher) Run(ctx context.Context, handler EventHandler) error {
	const debounceWindow = 500 * time.Millisecond

	// runCtx is cancelled the moment Run starts returning (for ANY reason —
	// ctx-cancel, Events/Errors close, or an fsnotify error), so the dispatcher
	// goroutine and any timer blocked handing off an event are released.
	runCtx, runCancel := context.WithCancel(ctx)

	var (
		mu      sync.Mutex
		pending = map[string]*time.Timer{}
		ready   = make(chan WatchEvent)
	)

	stopPending := func() {
		mu.Lock()
		defer mu.Unlock()

		for path, timer := range pending {
			timer.Stop()
			delete(pending, path)
		}
	}

	var dispatcher sync.WaitGroup

	dispatcher.Add(1)

	go func() {
		defer dispatcher.Done()

		for {
			select {
			case <-runCtx.Done():
				return
			case event := <-ready:
				_ = handler(event)
			}
		}
	}()

	// Ordered teardown: cancel first so the dispatcher and any blocked timer
	// hand-off unblock, stop outstanding timers so none fires post-shutdown,
	// then wait for the dispatcher (and any in-flight handler) to finish.
	defer func() {
		runCancel()
		stopPending()
		dispatcher.Wait()
	}()

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

			// Watch directories created after boot, else edits inside subtrees
			// added to a long-running daemon are never observed (kqueue/inotify
			// watch a single directory per Add, not recursively). This runs before
			// the reindex-relevance filter below so a moved-in subtree of nodes is
			// still watched even though the create event names the (extensionless)
			// directory.
			if kind == EventCreate {
				instance.watchNewDir(raw.Name)
			}

			// Drop events on paths that can never change the index — a redirected
			// .log, a .txt, an image. Scheduling a reindex for them is wasted work
			// at best and, when the write lands inside the watched tree (e.g.
			// `tusk graph -v > graph.log`), a self-sustaining walk loop at worst:
			// each walk's own log line is a MODIFY that would schedule the next
			// walk. Node files, the manifest, and directories still pass.
			if !triggersReindex(raw.Name, relPath) {
				continue
			}

			scheduled := WatchEvent{Kind: kind, Path: relPath}

			mu.Lock()
			if existing, alreadyPending := pending[relPath]; alreadyPending {
				existing.Stop()
			}

			pending[relPath] = time.AfterFunc(debounceWindow, func() {
				mu.Lock()
				delete(pending, relPath)
				mu.Unlock()

				// Hand the event to the single dispatcher; abandon it on shutdown
				// so this timer goroutine never leaks waiting on a stopped
				// dispatcher.
				select {
				case ready <- scheduled:
				case <-runCtx.Done():
				}
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

// watchNewDir adds a directory created after boot — and every non-ignored
// directory beneath it — to the watch set. A single create event can stand for
// a whole moved-in subtree, and the OS watcher adds only the named directory,
// so the subtree is walked. Non-directory creates and ignored directories are
// skipped; per-directory Add failures are logged but not fatal.
func (instance *Watcher) watchNewDir(absPath string) {
	info, statErr := os.Stat(absPath)

	if statErr != nil || !info.IsDir() {
		return
	}

	_ = filepath.WalkDir(absPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() {
			return nil //nolint:nilerr // best-effort: skip unreadable entries, keep watching the rest
		}

		relPath, relErr := filepath.Rel(instance.root, path)

		if relErr == nil && instance.matcher != nil && instance.matcher.Matches(filepath.ToSlash(relPath), true) {
			return filepath.SkipDir
		}

		if addErr := instance.fsWatcher.Add(path); addErr != nil && instance.logger != nil {
			instance.logger.Warn("watcher: failed to watch new directory",
				"dir", filepath.ToSlash(relPath),
				"err", addErr.Error(),
			)
		}

		return nil
	})
}

// triggersReindex reports whether a filesystem event should schedule a reindex
// walk. Only paths that can affect the index qualify: node files
// (.md/.mdx/.html/.htm), the manifest (tusk.toml), and directories — whose
// create/rename/delete can move whole subtrees of nodes in or out. Any other
// existing regular file (a redirected .log, a .txt, an image, an extensionless
// Makefile) can never become a node, so its events are dropped; this closes the
// self-sustaining reindex loop a redirected log inside the tree would otherwise
// arm (each walk's own log write is a MODIFY that would schedule the next walk).
//
// The file-vs-directory split is decided by os.Stat rather than the extension so
// a directory whose name contains a dot ("archive.v2") is not mistaken for a
// foreign file. A vanished path (a delete or a rename's source) cannot be
// stat'd; those reindex conservatively so an orphaned subtree is reaped — and
// they can never loop, since a read-only walk never deletes or renames a vault
// file, so no delete/rename event is ever self-caused.
func triggersReindex(absPath, relPath string) bool {
	if relPath == workspace.ManifestFilename || index.IsIndexableExt(relPath) {
		return true
	}

	info, statErr := os.Stat(absPath)

	if statErr != nil {
		return true
	}

	return info.IsDir()
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
