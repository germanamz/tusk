package watcher_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/watcher"
)

func TestWatcher_EmitsCreateAndModify(test *testing.T) {
	root := test.TempDir()

	watcherInstance, newErr := watcher.New(root)

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
