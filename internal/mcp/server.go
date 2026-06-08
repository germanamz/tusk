package mcp

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/indexepoch"
	"github.com/germanamz/tusk/internal/lock"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/manifestepoch"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/version"
)

// serverInstructions is sent back in the MCP initialize response. Clients
// surface this to the model at session start, so it carries the load of
// telling agents that Tusk is local (not a remote service) and pointing at
// the tusk_help tool for everything else. Keep it short — the deep content
// lives in the tusk_help topics.
const serverInstructions = `Tusk is a LOCAL workspace indexer running over the current working directory.
There is no remote service. Every tool operates on:
  - markdown files under ./
  - the schema declared in ./tusk.toml
  - the SQLite index at ./.tusk/index.db

To declare new node-types or edge-types, edit ./tusk.toml directly (no MCP
tool for this), then call tusk_reindex.

If the index seems wedged or corrupt, call tusk_reset(confirm: true) to drop and
rebuild it from your files.

Call tusk_help() for an overview + topic index, or tusk_help(topic: "<name>")
for deep dives on: workflow, node-types, edge-types, manifest, filter, query, packs.`

// Server wraps mcp-go's server with a Tusk Runtime.
type Server struct {
	mu                sync.RWMutex // guards the runtime pointer; readers = handlers (whole body) + snapshotRuntime (brief), writer = swap
	resetMu           sync.Mutex   // serializes ALL index-replacement ops in-process (reset tool + sibling reopen) so the flock/write-lock acquisition orders cannot interleave into a deadlock
	reindexMu         sync.Mutex   // per-process reindex gate, held during reindex.Run walk/enqueue (file watcher now; tusk_reload in a later phase)
	seenEpoch         atomic.Int64 // last .tusk/epoch this daemon has converged to
	seenManifestEpoch atomic.Int64 // last .tusk/manifest-epoch this daemon has converged to
	runtime           *Runtime
	mcp               *server.MCPServer
	handlers          map[string]server.ToolHandlerFunc
}

// NewServer builds a Server, registers every Tusk tool, and returns it. The
// caller invokes ServeStdio or ServeSSE to start a transport.
func NewServer(runtime *Runtime) *Server {
	core := server.NewMCPServer(
		"tusk",
		version.Current,
		server.WithToolCapabilities(true),
		server.WithInstructions(serverInstructions),
	)

	srv := &Server{
		runtime:  runtime,
		mcp:      core,
		handlers: map[string]server.ToolHandlerFunc{},
	}

	initialEpoch, _ := indexepoch.Read(runtime.Root)
	srv.seenEpoch.Store(initialEpoch)

	initialManifestEpoch, _ := manifestepoch.Read(runtime.Root)
	srv.seenManifestEpoch.Store(initialManifestEpoch)

	registerTools(srv)

	return srv
}

// snapshotRuntime returns the current runtime pointer under a brief read-lock.
// Background goroutines (drainers, watcher) call this each tick and then run
// their drain/reindex pass on the returned snapshot WITHOUT holding the lock —
// so a reset's write-lock is never blocked by a long Ollama-bound pass. If a
// concurrent swap closes the snapshot's handle mid-pass, database/sql.Close
// first lets the in-flight query finish (no panic), then subsequent queries
// error; the drainer logs and re-snapshots next tick (the index is a cache).
// Handlers do NOT use this — they hold the read-lock for their whole body (via
// the guarded register) so they never observe a closed handle.
func (srv *Server) snapshotRuntime() *Runtime {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	return srv.runtime
}

// register adds tool to the mcp-go server and srv.handlers, wrapping the handler
// so it holds the runtime read-lock for its entire duration. Use registerWrite
// for tools that must take the write-lock themselves (e.g. tusk_reset).
func (srv *Server) register(tool mcpgo.Tool, handler server.ToolHandlerFunc) {
	guarded := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		srv.mu.RLock()
		defer srv.mu.RUnlock()

		return handler(ctx, request)
	}

	srv.mcp.AddTool(tool, guarded)
	srv.handlers[tool.Name] = guarded
}

// registerWrite registers a handler WITHOUT the read-lock wrapper, for tools
// that acquire the write-lock internally (a read-locked handler taking the
// write-lock would deadlock) or that run a long pass off a snapshot. Used by
// tusk_reindex (snapshots, runs off the lock) and by tusk_reset in Phase 6.
func (srv *Server) registerWrite(tool mcpgo.Tool, handler server.ToolHandlerFunc) {
	srv.mcp.AddTool(tool, handler)
	srv.handlers[tool.Name] = handler
}

// installStoreLocked builds a fresh Runtime from an already-open store (reusing
// the current root/paths/logger/introspector and manifest) and swaps it in as
// srv.runtime. It does NOT close the old handle — the CALLER (which must hold
// srv.mu write) owns the old handle's lifecycle: the destructive tusk_reset
// closes it before deleting; the non-destructive reopenInPlace / siblingReopen
// close it only after a successful swap. Shared by reopenInPlace, the tusk_reset
// tool (Phase 6), and siblingReopen (Phase 7).
func (srv *Server) installStoreLocked(store *index.Index, manifestOverride *manifest.Manifest) error {
	old := srv.runtime

	fresh := &Runtime{
		Root:              old.Root,
		ManifestPath:      old.ManifestPath,
		IndexPath:         old.IndexPath,
		Logger:            old.Logger,
		aliasIntrospector: old.aliasIntrospector,
	}

	if buildErr := fresh.buildFromStore(store, manifestOverride); buildErr != nil {
		return buildErr
	}

	srv.runtime = fresh

	return nil
}

// reopenInPlace closes the current index handle and reopens the SAME path under
// the write-lock — the NON-DESTRUCTIVE swap (it deletes nothing). The write-lock
// waits for in-flight read-locked handlers (so no handler observes a closed
// handle); a background drainer mid-pass holds only a snapshot, and
// database/sql.Close lets its in-flight query finish before the close completes,
// after which it errors and re-snapshots next tick. resetMu serializes this
// against the tusk_reset tool (Phase 6) and siblingReopen (Phase 7) so their
// flock / write-lock acquisition orders cannot interleave into a deadlock.
func (srv *Server) reopenInPlace() error {
	srv.resetMu.Lock()
	defer srv.resetMu.Unlock()

	srv.mu.Lock()
	defer srv.mu.Unlock()

	old := srv.runtime

	// Open the new handle BEFORE closing the old one, and swap BEFORE closing,
	// so a failed open/rebuild leaves the old handle installed and live — the
	// server keeps serving rather than being left on a closed DB.
	store, openErr := index.Open(old.IndexPath)

	if openErr != nil {
		return fmt.Errorf("mcp: reopen: %w", openErr)
	}

	if installErr := srv.installStoreLocked(store, old.Manifest); installErr != nil {
		_ = store.Close()

		return installErr
	}

	_ = old.Index.Close()

	return nil
}

// siblingReopen reacts to another process having reset the shared index. It
// serializes on resetMu (against the local tusk_reset tool), awaits the resetter
// by acquiring the cross-process flock (which the resetter holds across
// delete→recreate→epoch-bump), then swaps to a fresh handle under the write-lock.
//
// If the index file is absent when the flock is acquired, the resetter crashed
// mid-reset; this daemon becomes the recreator: it reopens (which recreates the
// file), bumps the epoch, and kicks an Async rebuild. Otherwise it joins the
// already-recreated file and lets the lease-coordinated drainers rebuild.
//
// On flock-acquire timeout (a hung-but-alive resetter) it returns lock.ErrBusy
// WITHOUT closing the current handle, so the daemon keeps serving (stale) and
// retries on the next tick.
func (srv *Server) siblingReopen(ctx context.Context, lockTTL time.Duration) error {
	srv.resetMu.Lock()
	defer srv.resetMu.Unlock()

	srv.mu.RLock()
	root := srv.runtime.Root
	indexPath := srv.runtime.IndexPath
	srv.mu.RUnlock()

	// Already converged? When both watchers (RunEpochWatcher + the Phase 8
	// fast-path) detect the same bump, the first siblingReopen updates seenEpoch;
	// once we hold resetMu it is stable, so a stale second call is a cheap no-op.
	// (This compares the .tusk/epoch sentinel, not the index file — it never skips
	// a genuine new bump.)
	if latest, _ := indexepoch.Read(root); latest <= srv.seenEpoch.Load() {
		return nil
	}

	lockHandle, lockErr := lock.NewWorkspaceLock(root)
	if lockErr != nil {
		return fmt.Errorf("mcp: sibling reopen lock: %w", lockErr)
	}

	acquireCtx, cancel := context.WithTimeout(ctx, lockTTL)
	defer cancel()

	if acquireErr := lockHandle.Acquire(acquireCtx); acquireErr != nil {
		return acquireErr // ErrBusy: keep the old handle, retry next tick
	}

	defer func() { _ = lockHandle.Release() }()

	// Re-read manifest-epoch under the flock. If it advanced past
	// seenManifestEpoch, load and validate the fresh manifest OFF the write-lock
	// (so read-locked handlers never block on TOML parsing) and pass it to
	// installStoreLocked below; otherwise reuse the current manifest unchanged.
	// This converges the (index, manifest) pair atomically when a reset and a
	// reload land in the same window, so the daemon never serves the fresh index
	// against the stale manifest. resetMu + the flock are held across this whole
	// section, so srv.runtime is stable between this snapshot and the swap below.
	srv.mu.RLock()
	manifestPath := srv.runtime.ManifestPath
	aliasIntrospector := srv.runtime.aliasIntrospector
	freshManifest := srv.runtime.Manifest
	srv.mu.RUnlock()

	latestManifestEpoch, _ := manifestepoch.Read(root)
	manifestAdvanced := false
	if latestManifestEpoch > srv.seenManifestEpoch.Load() {
		loaded, loadErr := manifest.Load(manifestPath)

		if loadErr == nil {
			manifest.MergeBuiltinPacks(loaded)

			if aliasIntrospector != nil {
				manifest.ValidateAliases(loaded, aliasIntrospector)
				manifest.ValidateContext(loaded, aliasIntrospector)
			}

			// Gate on behavior engine build (blocking), not on alias/context (non-blocking).
			if _, buildErr := buildBehaviorEngine(loaded); buildErr == nil {
				freshManifest = loaded
				manifestAdvanced = true
			}
		}
	}

	_, statErr := os.Stat(indexPath)
	fileAbsent := os.IsNotExist(statErr)

	srv.mu.Lock()
	old := srv.runtime

	// Open the new handle BEFORE closing the old one, and swap BEFORE closing, so
	// a failed open/rebuild leaves the old handle installed and live — the daemon
	// keeps serving rather than being left on a closed DB (matches reopenInPlace).
	// On the recreator path (file absent) index.Open recreates the file here.
	store, openErr := index.Open(indexPath)
	if openErr != nil {
		srv.mu.Unlock()

		return fmt.Errorf("mcp: sibling reopen: %w", openErr)
	}

	if installErr := srv.installStoreLocked(store, freshManifest); installErr != nil {
		_ = store.Close()
		srv.mu.Unlock()

		return installErr
	}

	// Record the manifest epoch only AFTER a successful swap, so a failed install
	// leaves seenManifestEpoch unchanged and the next tick retries the reload.
	if manifestAdvanced {
		srv.seenManifestEpoch.Store(latestManifestEpoch)
	}

	fresh := srv.runtime  // freshly-installed runtime (used by the recreator branch)
	_ = old.Index.Close() // close the old handle only after a successful swap
	srv.mu.Unlock()

	if fileAbsent {
		// We are the recreator (resetter died mid-reset). Bump the epoch so other
		// siblings converge, and rebuild from disk.
		bumped, bumpErr := indexepoch.Bump(root)
		if bumpErr != nil {
			return fmt.Errorf("mcp: recreator bump: %w", bumpErr)
		}

		srv.seenEpoch.Store(bumped)

		if _, runErr := reindex.Run(reindex.Config{
			Root:            fresh.Root,
			Repo:            fresh.Nodes,
			Edges:           fresh.Edges,
			EdgeTypes:       fresh.Manifest.EdgeTypes,
			WorkspaceIgnore: fresh.Manifest.Workspace.Ignore,
			EmbedQueue:      fresh.EmbedQueue,
			Meta:            fresh.Meta,
			FileStates:      fresh.FileState,
			Workers:         fresh.Workers,
			Async:           true,
		}); runErr != nil {
			return fmt.Errorf("mcp: recreator reindex: %w", runErr)
		}

		return nil
	}

	// Joined an already-recreated file: record the resetter's epoch.
	current, _ := indexepoch.Read(root)
	srv.seenEpoch.Store(current)

	return nil
}

// maybeReopenForEpoch reopens the index if .tusk/epoch advanced beyond the
// last-seen value (another process reset it). Returns true if a reopen happened.
func (srv *Server) maybeReopenForEpoch(ctx context.Context, lockTTL time.Duration) (bool, error) {
	srv.mu.RLock()
	root := srv.runtime.Root
	srv.mu.RUnlock()

	current, readErr := indexepoch.Read(root)
	if readErr != nil {
		return false, readErr
	}

	if current <= srv.seenEpoch.Load() {
		return false, nil
	}

	if reopenErr := srv.siblingReopen(ctx, lockTTL); reopenErr != nil {
		return false, reopenErr
	}

	return true, nil
}

// maybeReloadManifestForEpoch reloads the manifest if .tusk/manifest-epoch
// advanced beyond the last-seen value (another process reloaded it). Returns
// true if a reload happened. Called by the two manifest watchers. Mirrors
// maybeReopenForEpoch but targets the manifest, not the index.
func (srv *Server) maybeReloadManifestForEpoch(ctx context.Context, lockTTL time.Duration) (bool, error) {
	srv.mu.RLock()
	root := srv.runtime.Root
	srv.mu.RUnlock()

	current, readErr := manifestepoch.Read(root)

	if readErr != nil {
		return false, readErr
	}

	if current <= srv.seenManifestEpoch.Load() {
		return false, nil
	}

	if reloadErr := srv.siblingReloadManifest(ctx, lockTTL); reloadErr != nil {
		return false, reloadErr
	}

	return true, nil
}

// siblingReloadManifest reacts to another process having reloaded the manifest.
// It serializes on resetMu (against local index swaps), awaits the reloader by
// acquiring the cross-process flock (which the reloader holds across load→
// validate→swap→epoch-bump), then delegates to buildReloaded (which loads +
// validates + builds a fresh Runtime reusing the open Index/repos), swaps the
// pointer under the write-lock, and records the epoch only on success. Sibling
// does NOT reindex (locked decision #4).
//
// buildReloaded gates on blocking validation (parse + behavior-engine) and is
// lenient on alias/context (recorded on the fresh Manifest, swap still proceeds);
// on a blocking failure it returns an error and seenManifestEpoch is left
// unchanged so the next tick retries once tusk.toml is valid again.
//
// On flock-acquire timeout it returns lock.ErrBusy WITHOUT advancing seenManifestEpoch,
// so the daemon keeps serving (stale manifest) and retries on the next tick.
func (srv *Server) siblingReloadManifest(ctx context.Context, lockTTL time.Duration) error {
	srv.resetMu.Lock()
	defer srv.resetMu.Unlock()

	srv.mu.RLock()
	root := srv.runtime.Root
	srv.mu.RUnlock()

	// Already converged? Dedup when both watchers detect the same bump.
	if latest, _ := manifestepoch.Read(root); latest <= srv.seenManifestEpoch.Load() {
		return nil
	}

	lockHandle, lockErr := lock.NewWorkspaceLock(root)

	if lockErr != nil {
		return fmt.Errorf("mcp: sibling reload lock: %w", lockErr)
	}

	acquireCtx, cancel := context.WithTimeout(ctx, lockTTL)
	defer cancel()

	if acquireErr := lockHandle.Acquire(acquireCtx); acquireErr != nil {
		return acquireErr // ErrBusy: keep the old manifest, retry next tick
	}

	defer func() { _ = lockHandle.Release() }()

	// Snapshot the current runtime, then load + validate + build a fresh Runtime
	// OFF the write-lock (buildReloaded does the TOML parse + validation), so
	// readers never block on parsing. buildReloaded reuses the open Index/repos.
	srv.mu.RLock()
	old := srv.runtime
	srv.mu.RUnlock()

	fresh, _, buildErr := old.buildReloaded()

	if buildErr != nil {
		return fmt.Errorf("mcp: sibling reload: %w", buildErr)
	}

	// Swap the pointer under the write-lock and record the epoch on success.
	// The fresh Runtime reuses the open Index and the same drift repos, so there
	// is nothing on old to close here — the index handle stays live.
	srv.mu.Lock()
	srv.runtime = fresh
	latestEpoch, _ := manifestepoch.Read(root)
	srv.seenManifestEpoch.Store(latestEpoch)
	srv.mu.Unlock()

	return nil
}

// HandleToolCall is exported for tests; production code goes through stdio/SSE.
// It dispatches to the registered handler for request.Params.Name. Returns an
// "unknown tool" CallToolResult error when the tool isn't registered.
func (srv *Server) HandleToolCall(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	handler, exists := srv.handlers[request.Params.Name]

	if !exists {
		return mcpgo.NewToolResultError(fmt.Sprintf("unknown tool %q", request.Params.Name)), nil
	}

	return handler(ctx, request)
}

// RegisteredToolNames returns the set of tool names currently registered on
// the server. Order is unspecified. Exposed so cross-package tests can
// cross-check the CLI/MCP wiring without duplicating tool-name lists.
func (srv *Server) RegisteredToolNames() []string {
	names := make([]string, 0, len(srv.handlers))

	for name := range srv.handlers {
		names = append(names, name)
	}

	return names
}

// ServeStdio runs the server over stdio. Blocks until stdin closes.
func (srv *Server) ServeStdio() error {
	return server.ServeStdio(srv.mcp)
}

// ServeSSE runs the server over SSE on addr (e.g. ":8765"). Blocks.
func (srv *Server) ServeSSE(addr string) error {
	sse := server.NewSSEServer(srv.mcp)

	return sse.Start(addr)
}

// RunBackground starts the background goroutines for this daemon. The two
// drainers and the file watcher are gated on runtime.Workers > 0 (they produce
// and consume reindex work). The two epoch-watcher pairs (index + manifest)
// always run — convergence is a consistency property, not an indexing one, so a
// read-only (Workers=0) daemon must still pick up a sibling's reset (index-epoch)
// and reload (manifest-epoch) rather than serve a stale index or schema. The
// epoch watchers are lightweight (a 2s poll + an fsnotify watch on .tusk/).
// Blocks until ctx cancels, then returns the first non-nil error.
func (srv *Server) RunBackground(ctx context.Context) error {
	var (
		mu    sync.Mutex
		first error
	)

	record := func(err error) {
		if err == nil {
			return
		}

		mu.Lock()
		defer mu.Unlock()

		if first == nil {
			first = err
		}
	}

	var waitGroup sync.WaitGroup

	srv.mu.RLock()
	workers := srv.runtime.Workers
	logger := srv.runtime.Logger
	srv.mu.RUnlock()

	// Resource-heavy passes — drainers + file watcher — require workers.
	if workers > 0 {
		waitGroup.Add(3)

		go func() {
			defer waitGroup.Done()
			record(RunDrainer(ctx, DrainerConfig{Server: srv, Logger: logger}))
		}()

		go func() {
			defer waitGroup.Done()
			record(RunReindexDrainer(ctx, ReindexDrainerConfig{Server: srv, Logger: logger}))
		}()

		go func() {
			defer waitGroup.Done()
			record(RunWatcher(ctx, WatchConfig{Server: srv, Logger: logger}))
		}()
	} else if logger != nil {
		logger.Warn(
			"embed workers disabled; content indexing and embedding are disabled in this instance. " +
				"Schema (node-types, edge-types, behaviors) and index resets still converge automatically, " +
				"but ensure another instance (or scheduled tusk reindex) drives content indexing for this " +
				"workspace, otherwise the index will go stale.")
	}

	// Epoch watchers (index + manifest) always run so read-only daemons converge.
	waitGroup.Add(4)

	go func() {
		defer waitGroup.Done()
		record(RunEpochWatcher(ctx, EpochWatchConfig{Server: srv, Logger: logger}))
	}()

	go func() {
		defer waitGroup.Done()
		record(RunIndexEpochFastWatcher(ctx, EpochWatchConfig{Server: srv, Logger: logger}))
	}()

	go func() {
		defer waitGroup.Done()
		record(RunManifestEpochWatcher(ctx, EpochWatchConfig{Server: srv, Logger: logger}))
	}()

	go func() {
		defer waitGroup.Done()
		record(RunManifestEpochFastWatcher(ctx, EpochWatchConfig{Server: srv, Logger: logger}))
	}()

	waitGroup.Wait()

	return first
}

// MCP exposes the underlying mcp-go server (for advanced wiring/tests).
func (srv *Server) MCP() *server.MCPServer {
	return srv.mcp
}
