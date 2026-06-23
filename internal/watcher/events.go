// Package watcher wraps fsnotify and emits debounced WatchEvents into a
// caller-supplied EventHandler. Used by `tusk watch`.
package watcher

// EventKind classifies a WatchEvent.
type EventKind int

const (
	EventCreate EventKind = iota
	EventModify
	EventRename // the renamed-to path is Path
	EventDelete
)

// WatchEvent is the unit of change emitted by Watcher.
type WatchEvent struct {
	Kind EventKind
	Path string // workspace-relative
}

// EventHandler is invoked synchronously by Watcher for each debounced event.
type EventHandler func(event WatchEvent) error
