---
type: plan
title: Plan 6
status: shipped
pr: 357
shipped-at: "2026-05-06"
implements:
  - Tusk v1 Rebuild
---

# Tusk v1 — Plan 6: MCP Server

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the long-running `tusk mcp` server — stdio + SSE transports, a 1:1 MCP tool surface mirroring the v1 CLI verbs, a background embed-queue drainer that replaces Plan 5's one-shot drain, and an integrated fsnotify watcher so external edits keep the index live. Plan 6 also lands the missing CLI verbs (`tusk node modify`, `tusk doctor`, `tusk status`) so the spec's two surfaces stay symmetric.

**Architecture:** A new `internal/mcp` package owns the server lifecycle: a `Runtime` value bundles the workspace, manifest, index, and engine services; a `Server` registers MCP tools backed by `Runtime` and serves them over stdio or SSE via `github.com/mark3labs/mcp-go`. Each mutation tool acquires a per-write workspace lock (matching spec §9.8 — locks are per-write, not per-session). A new `internal/embed/drain.go` file houses the queue-drainer logic extracted from `internal/reindex/reindex.go` so the MCP server can run it on a ticker. Watcher integration lives in `internal/mcp/watch.go`, reusing `internal/watcher` from Plan 3. Engine prerequisites: `node.Service` gains a `Modify` method; `internal/doctor` and `internal/status` provide readonly summaries; a `meta(key, value)` table records `last_reindex_at`.

**Tech Stack:** Go 1.26, the existing `internal/{workspace,manifest,index,node,filter,reindex,embed,watcher,lock}` packages, plus one new dependency: `github.com/mark3labs/mcp-go v0.52.0`. SSE listens on `localhost:<port>` (default `:8765`). Stdio is the default transport.

**Spec reference:** `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §11.3 (MCP surface), §9.2 (file watcher), §9.8 (concurrency), §10.6 (async embedding pipeline).

**Style rules:** Code respects `STYLE.md` — minimum 2-character identifiers (`*testing.T` → `test *testing.T`), blank lines around `err` guards, named errors on shadow.

---

## File Structure

**Created:**
```
internal/embed/
  drain.go               # DrainQueue(ctx, DrainConfig) drains embed_queue once;
                         # extracted from internal/reindex/reindex.go
  drain_test.go

internal/index/
  meta_repo.go           # MetaRepo: Get(key) / Set(key, value) over `meta` table
  meta_repo_test.go

internal/doctor/
  doctor.go              # Run(Config) (*Report, error)
                         # checks: dangling edges, embed-queue depth + retries
  doctor_test.go

internal/status/
  status.go              # Snapshot(Config) (*Snapshot, error)
                         # returns counts by type, edge count, queue depth, last-reindex
  status_test.go

internal/mcp/
  runtime.go             # Runtime holds workspace+manifest+index+services; Open/Close
  runtime_test.go
  server.go              # Server boots the mcp-go server, wires Runtime
  server_test.go
  tools.go               # tool registration + handlers (one file)
  tools_test.go
  drainer.go             # background goroutine that drains embed_queue every N seconds
  drainer_test.go
  watch.go               # fsnotify integration: external edits trigger partial reindex
  watch_test.go

cmd/tusk/
  cmd_node_modify.go     # tusk node modify <id> --prop k=v --unset k
  cmd_node_modify_test.go
  cmd_doctor.go          # tusk doctor
  cmd_doctor_test.go
  cmd_status.go          # tusk status
  cmd_status_test.go
  cmd_mcp.go             # tusk mcp [--transport stdio|sse] [--addr :8765]
  cmd_mcp_test.go        # smoke tests for transport flag parsing
  e2e_mcp_test.go        # full session: spawn `tusk mcp` over stdio, drive a few tools
```

**Modified:**
```
go.mod / go.sum                     # add github.com/mark3labs/mcp-go v0.52.0
internal/index/index.go             # add `meta(key TEXT PRIMARY KEY, value TEXT)` table
internal/index/index_test.go        # assert `meta` table is present
internal/node/service.go            # add ModifyInput + Service.Modify
internal/node/service_test.go       # cover Modify
internal/reindex/reindex.go         # call exported embed.DrainQueue; record last_reindex_at via meta repo
internal/reindex/reindex_test.go    # adjust assertion for last_reindex_at
cmd/tusk/cmd_node.go                # register newNodeModifyCmd()
cmd/tusk/root.go                    # register newDoctorCmd, newStatusCmd, newMCPCmd
cmd/tusk/cmd_reindex.go             # update meta repo after Run
```

**Excluded for Plan 6** (deferred per spec):
- `tusk_init` MCP tool — Plan 6's MCP server requires an existing workspace; bootstrap stays CLI-only. A follow-up plan can add it once we settle on hot-reload semantics for the runtime.
- Per-pack ergonomic MCP tools (`tusk_ticket_open`, `tusk_note_new`) — type packs aren't shipped yet (§8 placeholder); when packs land, their pack-specific MCP tools register through the same surface.
- Single-file partial reindex on watcher events — Plan 8 polish; Plan 6 keeps Plan 3's full-tree reindex on each event.
- `sqlite-vec` integration — out of scope; semantic queries continue to use Plan 5's pure-Go cosine over a structural prefilter.
- HTTP transport — spec §11.3 is explicit: stdio + SSE only.

---

## Module Conventions for Plan 6

**MCP library.** Every server/tool primitive comes from `github.com/mark3labs/mcp-go`. Package paths used in this plan:

```go
import (
    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)
```

Tool definitions use `mcp.NewTool(name, opts...)` with `mcp.WithDescription`, `mcp.WithString`, `mcp.WithNumber`, `mcp.WithBoolean`, `mcp.WithObject`, etc. Handlers have signature:

```go
type ToolHandler func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
```

Successful results return `mcp.NewToolResultText(jsonString)`. Failures return `mcp.NewToolResultError(message)`.

**Tool naming.** All tools use `tusk_<noun>_<verb>` per spec §11.3. Plan 6 ships:

```
tusk_status            tusk_node_create     tusk_edge_add
tusk_doctor            tusk_node_get        tusk_edge_remove
tusk_reindex           tusk_node_list       tusk_edge_list
tusk_query             tusk_node_modify
                       tusk_node_move
                       tusk_node_delete
```

`tusk_init` is intentionally omitted (see Excluded for Plan 6).

**Result encoding.** Every tool returns a single text content item containing a JSON object. Schema documented per-tool. JSON keys use snake_case (matches spec §10.8 result shape).

**Locking.** Every mutation tool wraps its body in `runtime.WithWriteLock(func() error { ... })`. Reads (`get`, `list`, `query`, `status`, `doctor`) skip the lock — SQLite WAL allows concurrent reads.

**Runtime contract:**
```go
type Runtime struct {
    Root         string
    ManifestPath string
    IndexPath    string

    Manifest    *manifest.Manifest
    Index       *index.Index
    Nodes       *index.NodeRepo
    Edges       *index.EdgeRepo
    EmbedQueue  *index.EmbedQueueRepo
    Embeddings  *index.EmbeddingRepo
    Meta        *index.MetaRepo
    NodeService *node.Service

    Embedder    embed.Embedder           // nil when [embeddings] absent
    Chunker     embed.ChunkingStrategy   // nil when [embeddings] absent
}

func Open(root string) (*Runtime, error)
func (rt *Runtime) Close() error
func (rt *Runtime) WithWriteLock(body func() error) error
func (rt *Runtime) ReloadManifest() error
```

`Open` builds every dependency in order (workspace → manifest → index → repos → services). `Close` releases the SQLite handle. `WithWriteLock` acquires/releases the per-write workspace lock (5-second timeout, mirrors `cmd_node.go:withWorkspaceLock`).

**Drainer contract:**
```go
// internal/mcp/drainer.go
type DrainerConfig struct {
    Runtime  *Runtime
    Interval time.Duration  // default 2*time.Second
    Logger   *slog.Logger   // optional; nil disables logging
}

func RunDrainer(ctx context.Context, config DrainerConfig) error
```

Loops on a ticker; calls `embed.DrainQueue` until the queue is empty; respects ctx cancellation.

**Watcher contract:**
```go
// internal/mcp/watch.go
type WatchConfig struct {
    Runtime *Runtime
    Logger  *slog.Logger
}

func RunWatcher(ctx context.Context, config WatchConfig) error
```

Boots an `internal/watcher.Watcher` rooted at `runtime.Root`; on each debounced event runs `reindex.Run(...)` (full-tree, like Plan 3's `cmd_watch.go`).

---

## Task 0: Pre-flight verification

- [ ] **Step 1: Confirm on `feat/plan-6` and clean tree**

```bash
git rev-parse --abbrev-ref HEAD
git status --short
git log --oneline -3
```

Expected: branch `feat/plan-6`; working tree clean (or only this plan doc); recent log starts with the v1 tip post-Plan-5 (`2e8fe9e feat(v1): plan 5 — semantic retrieval (#356)`).

- [ ] **Step 2: Confirm prior tests pass**

```bash
make test
make vet
```

Expected: all packages green, vet clean.

---

## Task 1: Index — `meta` key/value table

**Files:** Modify `internal/index/index.go`, `internal/index/index_test.go`.

- [ ] **Step 1: Append failing test to `internal/index/index_test.go`**

```go
func TestOpen_CreatesMetaTable(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, queryErr := store.ListTables()

	if queryErr != nil {
		test.Fatalf("ListTables: %v", queryErr)
	}

	if !contains(tables, "meta") {
		test.Errorf("missing table %q in %v", "meta", tables)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run TestOpen_CreatesMetaTable
```

Expected: FAIL — schema lacks the `meta` table.

- [ ] **Step 3: Append the table to the `schema` const in `internal/index/index.go`**

Append after the `warnings` table block, just before the closing backtick:

```sql
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -run TestOpen_CreatesMetaTable
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/index/index.go internal/index/index_test.go
git commit -m "feat(index): add meta key/value table"
```

---

## Task 2: Index — `MetaRepo`

**Files:** Create `internal/index/meta_repo.go`, `internal/index/meta_repo_test.go`.

- [ ] **Step 1: Write the failing test**

`internal/index/meta_repo_test.go`:

```go
package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestMetaRepo_GetMissingReturnsEmpty(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	value, getErr := repo.Get("missing")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if value != "" {
		test.Errorf("expected empty value, got %q", value)
	}
}

func TestMetaRepo_SetThenGet(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	if setErr := repo.Set("last_reindex_at", "1747000000"); setErr != nil {
		test.Fatalf("Set: %v", setErr)
	}

	value, getErr := repo.Get("last_reindex_at")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if value != "1747000000" {
		test.Errorf("value = %q", value)
	}
}

func TestMetaRepo_SetUpserts(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	if setErr := repo.Set("k", "v1"); setErr != nil {
		test.Fatalf("Set v1: %v", setErr)
	}

	if setErr := repo.Set("k", "v2"); setErr != nil {
		test.Fatalf("Set v2: %v", setErr)
	}

	value, _ := repo.Get("k")

	if value != "v2" {
		test.Errorf("expected v2, got %q", value)
	}
}

func openTempIndex(test *testing.T) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run TestMetaRepo
```

Expected: FAIL — `index.NewMetaRepo` undefined.

- [ ] **Step 3: Write `internal/index/meta_repo.go`**

```go
package index

import (
	"database/sql"
	"fmt"
)

// MetaRepo persists workspace-scoped key/value pairs in the `meta` table.
type MetaRepo struct {
	db *sql.DB
}

// NewMetaRepo constructs a MetaRepo backed by idx.
func NewMetaRepo(idx *Index) *MetaRepo {
	return &MetaRepo{db: idx.DB()}
}

// Get returns the value for key. Missing keys return ("", nil).
func (repo *MetaRepo) Get(key string) (string, error) {
	var value string

	scanErr := repo.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)

	if scanErr == sql.ErrNoRows {
		return "", nil
	}

	if scanErr != nil {
		return "", fmt.Errorf("metaRepo: get %s: %w", key, scanErr)
	}

	return value, nil
}

// Set upserts the value for key.
func (repo *MetaRepo) Set(key, value string) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)

	if execErr != nil {
		return fmt.Errorf("metaRepo: set %s: %w", key, execErr)
	}

	return nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -run TestMetaRepo -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/index/meta_repo.go internal/index/meta_repo_test.go
git commit -m "feat(index): MetaRepo for key/value workspace metadata"
```

---

## Task 3: Reindex — record `last_reindex_at`

**Files:** Modify `internal/reindex/reindex.go`, `internal/reindex/reindex_test.go`, `cmd/tusk/cmd_reindex.go`.

- [ ] **Step 1: Append a failing test to `internal/reindex/reindex_test.go`**

```go
func TestRun_RecordsLastReindexAt(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	metaRepo := index.NewMetaRepo(store)

	if _, runErr := reindex.Run(reindex.Config{
		Root: root,
		Repo: index.NewNodeRepo(store),
		Meta: metaRepo,
	}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	stored, getErr := metaRepo.Get("last_reindex_at")

	if getErr != nil {
		test.Fatalf("meta Get: %v", getErr)
	}

	if stored == "" {
		test.Errorf("expected last_reindex_at to be set")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/reindex/... -run TestRun_RecordsLastReindexAt
```

Expected: FAIL — `Config.Meta` undefined.

- [ ] **Step 3: Extend `reindex.Config`**

In `internal/reindex/reindex.go`, add after `Chunker embed.ChunkingStrategy`:

```go
	// Meta is optional; when set, Run records `last_reindex_at` (unix nanoseconds
	// formatted as decimal string) at the end of every successful pass.
	Meta *index.MetaRepo
```

- [ ] **Step 4: Update `Run` to record the timestamp**

At the very end of `Run` (just before `return report, nil`), insert:

```go
	if config.Meta != nil {
		if setErr := config.Meta.Set("last_reindex_at", fmt.Sprintf("%d", time.Now().UnixNano())); setErr != nil {
			return nil, fmt.Errorf("reindex: record last_reindex_at: %w", setErr)
		}
	}
```

Add `"time"` to the import block if not already present.

- [ ] **Step 5: Wire `Meta` through `cmd/tusk/cmd_reindex.go`**

In the `reindex.Run(reindex.Config{...})` call, add:

```go
		Meta: index.NewMetaRepo(store),
```

- [ ] **Step 6: Run, verify pass**

```bash
go test ./internal/reindex/... -run TestRun_RecordsLastReindexAt
make test
```

Expected: PASS for new test + every other package green.

- [ ] **Step 7: Commit**

```bash
git add internal/reindex cmd/tusk/cmd_reindex.go
git commit -m "feat(reindex): record last_reindex_at via MetaRepo"
```

---

## Task 4: Embed — extract `DrainQueue` from reindex

**Files:** Create `internal/embed/drain.go`, `internal/embed/drain_test.go`. Modify `internal/reindex/reindex.go`.

The `drainEmbedQueue` helper inside `internal/reindex/reindex.go` does the same job the MCP background drainer needs. Extract it to `internal/embed/` so both call sites share one implementation.

- [ ] **Step 1: Write the failing test**

`internal/embed/drain_test.go`:

```go
package embed_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
)

type stubEmbedder struct {
	calls   int
	dim     int
	model   string
	failure error
}

func (stub *stubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.calls++

	if stub.failure != nil {
		return nil, stub.failure
	}

	out := make([]float32, stub.dim)

	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}

func (stub *stubEmbedder) Model() string { return stub.model }
func (stub *stubEmbedder) Dim() int      { return stub.dim }

func TestDrainQueue_DrainsToEmpty(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "notes/a.md", "hi")
	createNodeFile(test, root, "notes/b.md", "ho")

	for _, id := range []string{"notes/a", "notes/b"} {
		nodeRepo.Upsert(index.NodeRow{ID: id, Type: "note", Path: id + ".md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})
		queueRepo.Enqueue(id)
	}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:          root,
		Nodes:         nodeRepo,
		Queue:         queueRepo,
		Embeddings:    embeddingRepo,
		Embedder:      &stubEmbedder{dim: 3, model: "stub"},
		Chunker:       embed.WholeDocument{},
		BatchSize:     50,
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 2 {
		test.Errorf("drained = %d, want 2", drained)
	}

	depth, _ := queueRepo.Depth()

	if depth != 0 {
		test.Errorf("depth = %d, want 0", depth)
	}
}

func TestDrainQueue_NoopWhenNoEmbedder(test *testing.T) {
	store := openIndex(test, test.TempDir())
	defer store.Close()

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Queue: index.NewEmbedQueueRepo(store),
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 0 {
		test.Errorf("drained = %d, want 0", drained)
	}
}

func openIndex(test *testing.T, root string) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(root, "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store
}

func createNodeFile(test *testing.T, root, relPath, body string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	frontmatter := "---\ntype: note\ntitle: x\n---\n\n" + body + "\n"

	if writeErr := os.WriteFile(abs, []byte(frontmatter), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}
}
```

Add `"os"` to the imports.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/embed/... -run TestDrainQueue
```

Expected: FAIL — `embed.DrainQueue` undefined.

- [ ] **Step 3: Write `internal/embed/drain.go`**

```go
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// DrainConfig configures DrainQueue.
type DrainConfig struct {
	Root       string                  // workspace root (required when Embedder is set)
	Nodes      *index.NodeRepo         // node repo for path lookups
	Queue      *index.EmbedQueueRepo   // queue repo (required)
	Embeddings *index.EmbeddingRepo    // embeddings repo (required when Embedder is set)
	Embedder   Embedder                // when nil, DrainQueue is a no-op
	Chunker    ChunkingStrategy        // required when Embedder is set
	BatchSize  int                     // optional; defaults to 50
}

// DrainQueue pops every pending row from embed_queue and embeds it. Returns the
// number of nodes successfully embedded. Failed rows are re-enqueued via
// MarkFailed. When DrainConfig.Embedder is nil, DrainQueue is a no-op.
//
// ctx cancellation aborts before the next batch; in-flight batches finish.
func DrainQueue(ctx context.Context, config DrainConfig) (int, error) {
	if config.Embedder == nil {
		return 0, nil
	}

	if config.Queue == nil {
		return 0, fmt.Errorf("embed: drain: Queue is required")
	}

	if config.Embeddings == nil {
		return 0, fmt.Errorf("embed: drain: Embeddings is required")
	}

	if config.Nodes == nil {
		return 0, fmt.Errorf("embed: drain: Nodes is required")
	}

	if config.Chunker == nil {
		return 0, fmt.Errorf("embed: drain: Chunker is required")
	}

	limit := config.BatchSize

	if limit <= 0 {
		limit = 50
	}

	var drained int

	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return drained, nil
		}

		batch, drainErr := config.Queue.Drain(limit)

		if drainErr != nil {
			return drained, drainErr
		}

		if len(batch) == 0 {
			return drained, nil
		}

		for _, queued := range batch {
			row, getErr := config.Nodes.Get(queued.NodeID)

			if getErr != nil {
				continue
			}

			content, readErr := os.ReadFile(filepath.Join(config.Root, row.Path))

			if readErr != nil {
				_ = config.Queue.Enqueue(queued.NodeID)
				_ = config.Queue.MarkFailed(queued.NodeID, readErr.Error())

				continue
			}

			parsed, parseErr := node.ParseFile(row.Path, content)

			if parseErr != nil {
				_ = config.Queue.Enqueue(queued.NodeID)
				_ = config.Queue.MarkFailed(queued.NodeID, parseErr.Error())

				continue
			}

			payload := BuildPayload(parsed)
			chunks := config.Chunker.Chunk(payload)

			if len(chunks) == 0 {
				continue
			}

			vector, embedErr := config.Embedder.Embed(ctx, chunks[0])

			if embedErr != nil {
				_ = config.Queue.Enqueue(queued.NodeID)
				_ = config.Queue.MarkFailed(queued.NodeID, embedErr.Error())

				continue
			}

			contentHash := sha256.Sum256(payload)

			if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
				NodeID:      queued.NodeID,
				ChunkIdx:    0,
				Model:       config.Embedder.Model(),
				ContentHash: hex.EncodeToString(contentHash[:]),
				Vector:      vector,
				Dim:         config.Embedder.Dim(),
			}); upsertErr != nil {
				return drained, upsertErr
			}

			drained++
		}
	}
}
```

- [ ] **Step 4: Replace the private `drainEmbedQueue` in `internal/reindex/reindex.go`**

Delete the entire `drainEmbedQueue` function and the `embedBatchSize` const at the bottom of `internal/reindex/reindex.go`. Find the call site (inside `Run`, after the walk loop) and replace it with:

```go
	if config.Embedder != nil {
		if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:       config.Root,
			Nodes:      config.Repo,
			Queue:      config.EmbedQueue,
			Embeddings: config.EmbeddingRepo,
			Embedder:   config.Embedder,
			Chunker:    config.Chunker,
		}); drainErr != nil {
			return nil, drainErr
		}
	}
```

If `context` was already imported, the line stays. Drop the `crypto/sha256` and `encoding/hex` imports if nothing else in the file uses them.

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/embed/... -run TestDrainQueue -v
go test ./internal/reindex/... -v
make test
```

Expected: every package green.

- [ ] **Step 6: Commit**

```bash
git add internal/embed/drain.go internal/embed/drain_test.go internal/reindex/reindex.go
git commit -m "feat(embed): extract DrainQueue from reindex into shared helper"
```

---

## Task 5: NodeService — `Modify` method

**Files:** Modify `internal/node/service.go`, `internal/node/service_test.go`.

- [ ] **Step 1: Append failing tests to `internal/node/service_test.go`**

```go
func TestService_Modify_UpdatesProperty(test *testing.T) {
	root := test.TempDir()
	store := openTempIndex(test, root)
	defer store.Close()

	service := node.NewServiceWithManifest(root, index.NewNodeRepo(store), index.NewEdgeRepo(store), manifest.EdgeTypes{})

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/hi.md",
		Type:    "note",
		Title:   "Hi",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	modified, modifyErr := service.Modify(node.ModifyInput{
		ID:        "notes/hi",
		SetProps:  map[string]any{"priority": 5},
		UnsetKeys: nil,
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if modified.Properties["priority"] != 5 {
		test.Errorf("priority = %v, want 5", modified.Properties["priority"])
	}

	contents, _ := os.ReadFile(filepath.Join(root, "notes/hi.md"))

	if !strings.Contains(string(contents), "priority: 5") {
		test.Errorf("file should contain priority: 5\n%s", contents)
	}
}

func TestService_Modify_UnsetRemovesProperty(test *testing.T) {
	root := test.TempDir()
	store := openTempIndex(test, root)
	defer store.Close()

	service := node.NewServiceWithManifest(root, index.NewNodeRepo(store), index.NewEdgeRepo(store), manifest.EdgeTypes{})

	if _, createErr := service.Create(node.CreateInput{
		RelPath:    "notes/hi.md",
		Type:       "note",
		Properties: map[string]any{"priority": 5},
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	modified, modifyErr := service.Modify(node.ModifyInput{
		ID:        "notes/hi",
		UnsetKeys: []string{"priority"},
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if _, hasPriority := modified.Properties["priority"]; hasPriority {
		test.Errorf("priority should be unset")
	}
}

func TestService_Modify_RejectsTypeChange(test *testing.T) {
	root := test.TempDir()
	store := openTempIndex(test, root)
	defer store.Close()

	service := node.NewServiceWithManifest(root, index.NewNodeRepo(store), index.NewEdgeRepo(store), manifest.EdgeTypes{})

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/hi.md",
		Type:    "note",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	_, modifyErr := service.Modify(node.ModifyInput{
		ID:       "notes/hi",
		SetProps: map[string]any{"type": "ticket"},
	})

	if modifyErr == nil {
		test.Fatalf("expected error rejecting type change")
	}
}

func TestService_Modify_EnqueuesEmbed(test *testing.T) {
	root := test.TempDir()
	store := openTempIndex(test, root)
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	service := node.NewServiceWithEmbedQueue(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		queueRepo,
	)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/hi.md", Type: "note"}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	queueRepo.Drain(100) // clear from Create

	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:       "notes/hi",
		SetProps: map[string]any{"priority": 1},
	}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	depth, _ := queueRepo.Depth()

	if depth != 1 {
		test.Errorf("depth = %d, want 1", depth)
	}
}
```

Add `"os"`, `"path/filepath"`, `"strings"` to imports if not already present (they are, but confirm).

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run TestService_Modify
```

Expected: FAIL — `node.ModifyInput` and `Service.Modify` undefined.

- [ ] **Step 3: Add `ModifyInput` and the `Modify` method to `internal/node/service.go`**

After `CreateInput`:

```go
// ModifyInput configures Service.Modify.
type ModifyInput struct {
	ID        string         // required; node id (path without extension)
	SetProps  map[string]any // properties to upsert (excluding "type"; modify rejects type changes)
	UnsetKeys []string       // top-level frontmatter keys to remove
	Body      *[]byte        // when non-nil, replaces the body; nil leaves body untouched
}
```

After the `Create` method (before `resolveTargetType`):

```go
// Modify reads a node from disk, applies SetProps/UnsetKeys/Body, validates
// against the manifest, atomically rewrites the file, and updates index rows.
// Modify enqueues the node for re-embedding when the service has an EmbedQueue.
func (service *Service) Modify(input ModifyInput) (*Node, error) {
	row, getErr := service.repo.Get(input.ID)

	if getErr != nil {
		return nil, getErr
	}

	absPath := filepath.Join(service.root, row.Path)

	original, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return nil, fmt.Errorf("node: read %s: %w", row.Path, readErr)
	}

	parsed, parseErr := ParseFile(row.Path, original)

	if parseErr != nil {
		return nil, parseErr
	}

	for _, key := range input.UnsetKeys {
		if key == "type" {
			return nil, fmt.Errorf("node: cannot unset reserved key %q", key)
		}

		delete(parsed.Properties, key)
	}

	for key, value := range input.SetProps {
		if key == "type" && value != parsed.Type {
			return nil, fmt.Errorf("node: cannot change type via Modify (current=%q, requested=%v)", parsed.Type, value)
		}

		parsed.Properties[key] = value
	}

	body := parsed.Body

	if input.Body != nil {
		body = *input.Body
		parsed.Body = body
	}

	rendered, renderErr := renderMarkdown(parsed.Properties, body)

	if renderErr != nil {
		return nil, renderErr
	}

	if writeErr := atomicWrite(absPath, rendered); writeErr != nil {
		return nil, fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		return nil, fmt.Errorf("node: stat %s: %w", absPath, statErr)
	}

	reparsed, reparseErr := ParseFile(row.Path, rendered)

	if reparseErr != nil {
		return nil, reparseErr
	}

	if resolveErr := ResolveEdges(reparsed, service.edgeTypes); resolveErr != nil {
		return nil, resolveErr
	}

	if validateErr := ValidateEdges(reparsed, service.edgeTypes, EdgeContext{
		ResolveTargetType: service.resolveTargetType,
	}); validateErr != nil {
		return nil, validateErr
	}

	if cycleErr := service.detectCyclesForAcyclicEdges(reparsed); cycleErr != nil {
		return nil, cycleErr
	}

	checksum := sha256Hex(rendered)
	propertiesJSON, marshalErr := json.Marshal(reparsed.Properties)

	if marshalErr != nil {
		return nil, fmt.Errorf("node: marshal properties: %w", marshalErr)
	}

	if upsertErr := service.repo.Upsert(index.NodeRow{
		ID:             reparsed.ID,
		Type:           reparsed.Type,
		Path:           reparsed.Path,
		Title:          reparsed.Title,
		PropertiesJSON: string(propertiesJSON),
		LastMtime:      stat.ModTime().UnixNano(),
		LastSize:       stat.Size(),
		LastChecksum:   checksum,
	}); upsertErr != nil {
		return nil, upsertErr
	}

	if service.edges != nil {
		if upsertErr := service.edges.UpsertAll(reparsed.ID, reparsed.Path, flattenEdges(reparsed)); upsertErr != nil {
			return nil, upsertErr
		}
	}

	if service.embedQueue != nil {
		if enqueueErr := service.embedQueue.Enqueue(reparsed.ID); enqueueErr != nil {
			return nil, enqueueErr
		}
	}

	return reparsed, nil
}

// atomicWrite writes content to a sibling temp file and renames over absPath.
func atomicWrite(absPath string, content []byte) error {
	dir := filepath.Dir(absPath)

	tempFile, createErr := os.CreateTemp(dir, ".tusk-modify-*")

	if createErr != nil {
		return createErr
	}

	tempPath := tempFile.Name()

	if _, writeErr := tempFile.Write(content); writeErr != nil {
		tempFile.Close()
		os.Remove(tempPath)

		return writeErr
	}

	if syncErr := tempFile.Sync(); syncErr != nil {
		tempFile.Close()
		os.Remove(tempPath)

		return syncErr
	}

	if closeErr := tempFile.Close(); closeErr != nil {
		os.Remove(tempPath)

		return closeErr
	}

	return os.Rename(tempPath, absPath)
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: 4 new PASS plus all prior tests green.

- [ ] **Step 5: Commit**

```bash
git add internal/node
git commit -m "feat(node): Service.Modify edits properties/body atomically"
```

---

## Task 6: CLI — `tusk node modify`

**Files:** Create `cmd/tusk/cmd_node_modify.go`, `cmd/tusk/cmd_node_modify_test.go`. Modify `cmd/tusk/cmd_node.go`.

- [ ] **Step 1: Write failing test**

`cmd/tusk/cmd_node_modify_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeModify_SetProperty(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/x.md", "note", "X", "")

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("node", "modify", "notes/x", "--prop", "priority=5")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	body, _ := os.ReadFile(filepath.Join(root, "notes/x.md"))

	if !strings.Contains(string(body), "priority: 5") {
		test.Errorf("file missing priority: 5\n%s", body)
	}
}

func TestNodeModify_UnsetProperty(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/x.md", "note", "X", "")

	chdir(test, root)
	defer chdir(test, "")

	if _, runErr := runCLI("node", "modify", "notes/x", "--prop", "priority=5"); runErr != nil {
		test.Fatalf("set: %v", runErr)
	}

	if _, runErr := runCLI("node", "modify", "notes/x", "--unset", "priority"); runErr != nil {
		test.Fatalf("unset: %v", runErr)
	}

	body, _ := os.ReadFile(filepath.Join(root, "notes/x.md"))

	if strings.Contains(string(body), "priority:") {
		test.Errorf("priority should be removed:\n%s", body)
	}
}

// runCLI is defined in e2e_test.go; setupTempWorkspace, createNode, chdir
// are existing helpers used by other cmd_*_test.go files.
var _ = bytes.Buffer{} // keep import alive when only one helper uses it
```

If your project lacks the helpers above, locate the existing pattern (e.g., `cmd_node_create_test.go`) and reuse the same helper names — DO NOT add new helpers without confirming.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestNodeModify
```

Expected: FAIL — `node modify` subcommand not registered.

- [ ] **Step 3: Write `cmd/tusk/cmd_node_modify.go`**

```go
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeModifyCmd() *cobra.Command {
	var (
		setFlags   []string
		unsetFlags []string
	)

	modifyCmd := &cobra.Command{
		Use:   "modify <id>",
		Short: "Modify a node's frontmatter properties",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			setProps, setErr := parseSetFlags(setFlags)

			if setErr != nil {
				return setErr
			}

			return withWorkspaceLock(ws, func() error {
				store, openErr := index.Open(ws.IndexPath)

				if openErr != nil {
					return openErr
				}

				defer store.Close()

				service := node.NewServiceWithEmbedQueue(
					ws.Root,
					index.NewNodeRepo(store),
					index.NewEdgeRepo(store),
					loaded.EdgeTypes,
					index.NewEmbedQueueRepo(store),
				)

				modified, modifyErr := service.Modify(node.ModifyInput{
					ID:        args[0],
					SetProps:  setProps,
					UnsetKeys: unsetFlags,
				})

				if modifyErr != nil {
					return modifyErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Modified %s\n", modified.ID)

				return nil
			})
		},
	}

	modifyCmd.Flags().StringArrayVar(&setFlags, "prop", nil, "set property: --prop key=value (repeatable)")
	modifyCmd.Flags().StringArrayVar(&unsetFlags, "unset", nil, "unset property: --unset key (repeatable)")

	return modifyCmd
}

// parseSetFlags converts ["k=v", "n=42", "b=true"] into a map[string]any with
// best-effort scalar typing (int, bool, then string).
func parseSetFlags(flags []string) (map[string]any, error) {
	props := map[string]any{}

	for _, raw := range flags {
		eq := strings.IndexByte(raw, '=')

		if eq <= 0 {
			return nil, fmt.Errorf("--prop: expected key=value, got %q", raw)
		}

		key := raw[:eq]
		value := raw[eq+1:]

		if intValue, parseErr := strconv.Atoi(value); parseErr == nil {
			props[key] = intValue

			continue
		}

		if boolValue, parseErr := strconv.ParseBool(value); parseErr == nil {
			props[key] = boolValue

			continue
		}

		props[key] = value
	}

	return props, nil
}
```

- [ ] **Step 4: Register in `cmd/tusk/cmd_node.go`**

In `newNodeCmd`, after the existing `AddCommand` calls, add:

```go
	nodeCmd.AddCommand(newNodeModifyCmd())
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestNodeModify -v
```

Expected: 2 PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_node.go cmd/tusk/cmd_node_modify.go cmd/tusk/cmd_node_modify_test.go
git commit -m "feat(cli): tusk node modify edits properties via flags"
```

---

## Task 7: `internal/doctor` — health checks

**Files:** Create `internal/doctor/doctor.go`, `internal/doctor/doctor_test.go`.

- [ ] **Step 1: Write failing test**

`internal/doctor/doctor_test.go`:

```go
package doctor_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
)

func TestRun_NoIssuesOnFreshIndex(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(report.Issues) != 0 {
		test.Errorf("expected 0 issues, got %d: %+v", len(report.Issues), report.Issues)
	}
}

func TestRun_FlagsDanglingEdges(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	nodeRepo.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", Title: "A", PropertiesJSON: "{}", LastChecksum: "x"})
	edgeRepo.UpsertAll("tickets/a", "tickets/a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/missing", Ordinal: 0, SourcePath: "tickets/a.md"},
	})

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      nodeRepo,
		Edges:      edgeRepo,
		EmbedQueue: index.NewEmbedQueueRepo(store),
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(report.Issues) == 0 {
		test.Fatalf("expected dangling edge to be flagged")
	}

	if report.Issues[0].Kind != doctor.IssueDanglingEdge {
		test.Errorf("kind = %q, want %q", report.Issues[0].Kind, doctor.IssueDanglingEdge)
	}
}

func TestRun_ReportsQueueDepth(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	queueRepo.Enqueue("notes/x")
	queueRepo.Enqueue("notes/y")

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: queueRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedQueueDepth != 2 {
		test.Errorf("EmbedQueueDepth = %d, want 2", report.EmbedQueueDepth)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/doctor/...
```

Expected: FAIL — package missing.

- [ ] **Step 3: Write `internal/doctor/doctor.go`**

```go
// Package doctor runs read-only health checks against the index.
package doctor

import (
	"fmt"

	"github.com/germanamz/tusk/internal/index"
)

// Issue kinds.
const (
	IssueDanglingEdge = "dangling-edge"
	IssueEmbedRetry   = "embed-retry"
)

// Issue is a single problem the doctor surfaced.
type Issue struct {
	Kind    string
	NodeID  string
	Message string
}

// Report is the doctor's verdict.
type Report struct {
	Issues          []Issue
	EmbedQueueDepth int
}

// Config configures Run.
type Config struct {
	Nodes      *index.NodeRepo
	Edges      *index.EdgeRepo
	EmbedQueue *index.EmbedQueueRepo
}

// Run executes every check and returns the aggregate Report.
func Run(config Config) (*Report, error) {
	report := &Report{}

	if config.Edges != nil && config.Nodes != nil {
		dangling, danglingErr := findDanglingEdges(config.Nodes, config.Edges)

		if danglingErr != nil {
			return nil, danglingErr
		}

		report.Issues = append(report.Issues, dangling...)
	}

	if config.EmbedQueue != nil {
		depth, depthErr := config.EmbedQueue.Depth()

		if depthErr != nil {
			return nil, depthErr
		}

		report.EmbedQueueDepth = depth
	}

	return report, nil
}

// findDanglingEdges scans every edge and flags those whose target_id has no
// node row.
func findDanglingEdges(nodes *index.NodeRepo, edges *index.EdgeRepo) ([]Issue, error) {
	allEdges, listErr := edges.ListAll()

	if listErr != nil {
		return nil, listErr
	}

	var issues []Issue

	known := map[string]struct{}{}

	for _, edge := range allEdges {
		known[edge.SourceID] = struct{}{}
	}

	// Cache existence checks: NodeRepo.Get returns an error when the row is
	// missing; cache positive lookups in a set, query on first miss.
	resolved := map[string]bool{}

	for _, edge := range allEdges {
		if cached, hit := resolved[edge.TargetID]; hit {
			if cached {
				continue
			}

			issues = append(issues, Issue{
				Kind:    IssueDanglingEdge,
				NodeID:  edge.SourceID,
				Message: fmt.Sprintf("edge %q -> %q (target missing)", edge.Type, edge.TargetID),
			})

			continue
		}

		if _, getErr := nodes.Get(edge.TargetID); getErr != nil {
			resolved[edge.TargetID] = false

			issues = append(issues, Issue{
				Kind:    IssueDanglingEdge,
				NodeID:  edge.SourceID,
				Message: fmt.Sprintf("edge %q -> %q (target missing)", edge.Type, edge.TargetID),
			})

			continue
		}

		resolved[edge.TargetID] = true
	}

	return issues, nil
}
```

- [ ] **Step 4: Add `EdgeRepo.ListAll`**

In `internal/index/edge_repo.go`, append (next to the other List methods):

```go
// ListAll returns every edge in the index, ordered by source_id then ordinal.
func (repo *EdgeRepo) ListAll() ([]EdgeRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT type, source_id, target_id, ordinal, source_path
		FROM edges
		ORDER BY source_id, ordinal
	`)

	if queryErr != nil {
		return nil, queryErr
	}

	defer rows.Close()

	var out []EdgeRow

	for rows.Next() {
		var row EdgeRow

		if scanErr := rows.Scan(&row.Type, &row.SourceID, &row.TargetID, &row.Ordinal, &row.SourcePath); scanErr != nil {
			return nil, scanErr
		}

		out = append(out, row)
	}

	return out, rows.Err()
}
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/doctor/... -v
go test ./internal/index/... -v
```

Expected: 3 PASS in doctor; index tests still green.

- [ ] **Step 6: Commit**

```bash
git add internal/doctor internal/index/edge_repo.go
git commit -m "feat(doctor): dangling-edge + embed-queue health checks"
```

---

## Task 8: CLI — `tusk doctor`

**Files:** Create `cmd/tusk/cmd_doctor.go`, `cmd/tusk/cmd_doctor_test.go`. Modify `cmd/tusk/root.go`.

- [ ] **Step 1: Write failing test**

`cmd/tusk/cmd_doctor_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestDoctor_PrintsCleanReport(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "no issues") {
		test.Errorf("expected 'no issues', got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestDoctor
```

Expected: FAIL — `doctor` subcommand missing.

- [ ] **Step 3: Write `cmd/tusk/cmd_doctor.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Surface validation warnings and index health issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			report, runErr := doctor.Run(doctor.Config{
				Nodes:      index.NewNodeRepo(store),
				Edges:      index.NewEdgeRepo(store),
				EmbedQueue: index.NewEmbedQueueRepo(store),
			})

			if runErr != nil {
				return runErr
			}

			out := cmd.OutOrStdout()

			if len(report.Issues) == 0 {
				_, _ = fmt.Fprintln(out, "doctor: no issues")
			}

			for _, issue := range report.Issues {
				_, _ = fmt.Fprintf(out, "  [%s] %s: %s\n", issue.Kind, issue.NodeID, issue.Message)
			}

			_, _ = fmt.Fprintf(out, "embed queue depth: %d\n", report.EmbedQueueDepth)

			return nil
		},
	}

	return doctorCmd
}
```

- [ ] **Step 4: Register in `cmd/tusk/root.go`**

After the other `AddCommand` calls in `newRootCmd`:

```go
	rootCmd.AddCommand(newDoctorCmd())
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestDoctor -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_doctor.go cmd/tusk/cmd_doctor_test.go cmd/tusk/root.go
git commit -m "feat(cli): tusk doctor surfaces dangling edges and queue depth"
```

---

## Task 9: `internal/status` — workspace summary

**Files:** Create `internal/status/status.go`, `internal/status/status_test.go`.

- [ ] **Step 1: Write failing test**

`internal/status/status_test.go`:

```go
package status_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/status"
)

func TestSnapshot_CountsByType(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodes := index.NewNodeRepo(store)

	nodes.Upsert(index.NodeRow{ID: "t/a", Type: "ticket", Path: "t/a.md", PropertiesJSON: "{}", LastChecksum: "x"})
	nodes.Upsert(index.NodeRow{ID: "t/b", Type: "ticket", Path: "t/b.md", PropertiesJSON: "{}", LastChecksum: "x"})
	nodes.Upsert(index.NodeRow{ID: "n/c", Type: "note", Path: "n/c.md", PropertiesJSON: "{}", LastChecksum: "x"})

	snap, snapErr := status.Snapshot(status.Config{
		Nodes:      nodes,
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Meta:       index.NewMetaRepo(store),
	})

	if snapErr != nil {
		test.Fatalf("Snapshot: %v", snapErr)
	}

	if snap.NodesByType["ticket"] != 2 {
		test.Errorf("ticket count = %d, want 2", snap.NodesByType["ticket"])
	}

	if snap.NodesByType["note"] != 1 {
		test.Errorf("note count = %d, want 1", snap.NodesByType["note"])
	}
}

func TestSnapshot_ReportsQueueDepth(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	queueRepo.Enqueue("a")
	queueRepo.Enqueue("b")
	queueRepo.Enqueue("c")

	snap, _ := status.Snapshot(status.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: queueRepo,
		Meta:       index.NewMetaRepo(store),
	})

	if snap.EmbedQueueDepth != 3 {
		test.Errorf("EmbedQueueDepth = %d, want 3", snap.EmbedQueueDepth)
	}
}

func TestSnapshot_LastReindexAt(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	metaRepo := index.NewMetaRepo(store)
	metaRepo.Set("last_reindex_at", "1747000000")

	snap, _ := status.Snapshot(status.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Meta:       metaRepo,
	})

	if snap.LastReindexAt != "1747000000" {
		test.Errorf("LastReindexAt = %q, want 1747000000", snap.LastReindexAt)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/status/...
```

Expected: FAIL — package missing.

- [ ] **Step 3: Write `internal/status/status.go`**

```go
// Package status renders a quick health summary of the workspace.
package status

import (
	"github.com/germanamz/tusk/internal/index"
)

// Snapshot describes the workspace at a moment in time.
type SnapshotData struct {
	NodesByType     map[string]int
	EdgeCount       int
	EmbedQueueDepth int
	LastReindexAt   string
}

// Config configures Snapshot.
type Config struct {
	Nodes      *index.NodeRepo
	Edges      *index.EdgeRepo
	EmbedQueue *index.EmbedQueueRepo
	Meta       *index.MetaRepo
}

// Snapshot reads index aggregates and returns the rolled-up SnapshotData.
func Snapshot(config Config) (*SnapshotData, error) {
	snap := &SnapshotData{NodesByType: map[string]int{}}

	nodes, listErr := config.Nodes.List(index.ListFilter{})

	if listErr != nil {
		return nil, listErr
	}

	for _, row := range nodes {
		snap.NodesByType[row.Type]++
	}

	edges, edgeErr := config.Edges.ListAll()

	if edgeErr != nil {
		return nil, edgeErr
	}

	snap.EdgeCount = len(edges)

	depth, depthErr := config.EmbedQueue.Depth()

	if depthErr != nil {
		return nil, depthErr
	}

	snap.EmbedQueueDepth = depth

	last, lastErr := config.Meta.Get("last_reindex_at")

	if lastErr != nil {
		return nil, lastErr
	}

	snap.LastReindexAt = last

	return snap, nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/status/... -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/status
git commit -m "feat(status): workspace snapshot (counts by type, queue depth, last reindex)"
```

---

## Task 10: CLI — `tusk status`

**Files:** Create `cmd/tusk/cmd_status.go`, `cmd/tusk/cmd_status_test.go`. Modify `cmd/tusk/root.go`.

- [ ] **Step 1: Write failing test**

`cmd/tusk/cmd_status_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestStatus_PrintsCounts(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "tickets/a.md", "ticket", "A", "")
	createNode(test, root, "tickets/b.md", "ticket", "B", "")
	createNode(test, root, "notes/c.md", "note", "C", "")

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("status")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "ticket") || !strings.Contains(out, "2") {
		test.Errorf("expected 'ticket … 2' in:\n%s", out)
	}

	if !strings.Contains(out, "note") || !strings.Contains(out, "1") {
		test.Errorf("expected 'note … 1' in:\n%s", out)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestStatus
```

Expected: FAIL.

- [ ] **Step 3: Write `cmd/tusk/cmd_status.go`**

```go
package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/status"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Quick workspace summary: node counts, edge count, queue depth, last reindex",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			snap, snapErr := status.Snapshot(status.Config{
				Nodes:      index.NewNodeRepo(store),
				Edges:      index.NewEdgeRepo(store),
				EmbedQueue: index.NewEmbedQueueRepo(store),
				Meta:       index.NewMetaRepo(store),
			})

			if snapErr != nil {
				return snapErr
			}

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintln(tab, "TYPE\tCOUNT")

			types := make([]string, 0, len(snap.NodesByType))

			for typeName := range snap.NodesByType {
				types = append(types, typeName)
			}

			sort.Strings(types)

			for _, typeName := range types {
				_, _ = fmt.Fprintf(tab, "%s\t%d\n", typeName, snap.NodesByType[typeName])
			}

			tab.Flush()

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "edges: %d\nembed queue depth: %d\nlast reindex (unix ns): %s\n",
				snap.EdgeCount, snap.EmbedQueueDepth, snap.LastReindexAt)

			return nil
		},
	}

	return statusCmd
}
```

- [ ] **Step 4: Register in `cmd/tusk/root.go`**

```go
	rootCmd.AddCommand(newStatusCmd())
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestStatus -v
make test
```

Expected: PASS for new test + every other package green.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_status.go cmd/tusk/cmd_status_test.go cmd/tusk/root.go
git commit -m "feat(cli): tusk status summarizes workspace"
```

---

## Task 11: Add `mark3labs/mcp-go` dependency

**Files:** Modify `go.mod`, `go.sum`.

- [ ] **Step 1: Add the dependency**

```bash
cd /workspaces/tusk
go get github.com/mark3labs/mcp-go@v0.52.0
go mod tidy
```

- [ ] **Step 2: Verify it builds**

```bash
go build ./...
```

Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): add github.com/mark3labs/mcp-go v0.52.0"
```

---

## Task 12: `internal/mcp` Runtime

**Files:** Create `internal/mcp/runtime.go`, `internal/mcp/runtime_test.go`.

- [ ] **Step 1: Write failing test**

`internal/mcp/runtime_test.go`:

```go
package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

func TestOpen_LoadsWorkspace(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.Manifest == nil {
		test.Errorf("Manifest is nil")
	}

	if rt.Index == nil {
		test.Errorf("Index is nil")
	}

	if rt.NodeService == nil {
		test.Errorf("NodeService is nil")
	}
}

func TestOpen_FailsWhenNoWorkspace(test *testing.T) {
	if _, openErr := mcp.Open(test.TempDir()); openErr == nil {
		test.Fatalf("expected error for missing tusk.toml")
	}
}

func TestRuntime_WithWriteLockSerializes(test *testing.T) {
	root := test.TempDir()

	os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644)

	rt, _ := mcp.Open(root)
	defer rt.Close()

	calls := 0

	if lockErr := rt.WithWriteLock(func() error {
		calls++

		return nil
	}); lockErr != nil {
		test.Fatalf("WithWriteLock: %v", lockErr)
	}

	if calls != 1 {
		test.Errorf("body should run once, got %d", calls)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/mcp/...
```

Expected: FAIL — package missing.

- [ ] **Step 3: Write `internal/mcp/runtime.go`**

```go
// Package mcp implements the long-running tusk MCP server.
package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/lock"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
)

// Runtime bundles every shared dependency the MCP server's tool handlers need.
type Runtime struct {
	Root         string
	ManifestPath string
	IndexPath    string

	Manifest    *manifest.Manifest
	Index       *index.Index
	Nodes       *index.NodeRepo
	Edges       *index.EdgeRepo
	EmbedQueue  *index.EmbedQueueRepo
	Embeddings  *index.EmbeddingRepo
	Meta        *index.MetaRepo
	NodeService *node.Service

	Embedder embed.Embedder
	Chunker  embed.ChunkingStrategy
}

// Open builds a Runtime rooted at workspaceRoot.
func Open(workspaceRoot string) (*Runtime, error) {
	ws, findErr := workspace.Find(workspaceRoot)

	if findErr != nil {
		return nil, fmt.Errorf("mcp: workspace: %w", findErr)
	}

	loaded, loadErr := manifest.Load(ws.ManifestPath)

	if loadErr != nil {
		return nil, fmt.Errorf("mcp: manifest: %w", loadErr)
	}

	store, openErr := index.Open(ws.IndexPath)

	if openErr != nil {
		return nil, fmt.Errorf("mcp: index: %w", openErr)
	}

	rt := &Runtime{
		Root:         ws.Root,
		ManifestPath: ws.ManifestPath,
		IndexPath:    ws.IndexPath,
		Manifest:     loaded,
		Index:        store,
		Nodes:        index.NewNodeRepo(store),
		Edges:        index.NewEdgeRepo(store),
		EmbedQueue:   index.NewEmbedQueueRepo(store),
		Embeddings:   index.NewEmbeddingRepo(store),
		Meta:         index.NewMetaRepo(store),
	}

	if loaded.Embeddings.Provider == "ollama" {
		rt.Embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
			Endpoint: loaded.Embeddings.Endpoint,
			Model:    loaded.Embeddings.Model,
			Dim:      loaded.Embeddings.Dim,
		})
		rt.Chunker = embed.WholeDocument{}
	}

	rt.NodeService = node.NewServiceWithEmbedQueue(
		rt.Root,
		rt.Nodes,
		rt.Edges,
		loaded.EdgeTypes,
		rt.EmbedQueue,
	)

	return rt, nil
}

// Close releases the index handle.
func (rt *Runtime) Close() error {
	if rt.Index == nil {
		return nil
	}

	return rt.Index.Close()
}

// WithWriteLock acquires the per-write workspace lock, runs body, and always
// releases. 5-second acquisition timeout.
func (rt *Runtime) WithWriteLock(body func() error) error {
	handle, newErr := lock.NewWorkspaceLock(rt.Root)

	if newErr != nil {
		return newErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if acquireErr := handle.Acquire(ctx); acquireErr != nil {
		return acquireErr
	}

	defer func() { _ = handle.Release() }()

	return body()
}

// ReloadManifest re-reads the manifest from disk and rebuilds the NodeService.
// Use after `tusk_reindex` or out-of-band manifest edits.
func (rt *Runtime) ReloadManifest() error {
	loaded, loadErr := manifest.Load(rt.ManifestPath)

	if loadErr != nil {
		return fmt.Errorf("mcp: reload manifest: %w", loadErr)
	}

	rt.Manifest = loaded
	rt.NodeService = node.NewServiceWithEmbedQueue(
		rt.Root,
		rt.Nodes,
		rt.Edges,
		loaded.EdgeTypes,
		rt.EmbedQueue,
	)

	return nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/mcp/... -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/runtime.go internal/mcp/runtime_test.go
git commit -m "feat(mcp): Runtime bundles workspace + index + services"
```

---

## Task 13: `internal/mcp` Server skeleton

**Files:** Create `internal/mcp/server.go`, `internal/mcp/server_test.go`.

- [ ] **Step 1: Write failing test**

`internal/mcp/server_test.go`:

```go
package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

func TestNewServer_ReturnsServer(test *testing.T) {
	root := test.TempDir()

	os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644)

	rt, _ := mcp.Open(root)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	if srv == nil {
		test.Fatalf("NewServer returned nil")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/mcp/... -run TestNewServer
```

Expected: FAIL — `NewServer` undefined.

- [ ] **Step 3: Write `internal/mcp/server.go`**

```go
package mcp

import (
	"github.com/mark3labs/mcp-go/server"
)

// Version reported by the MCP server in its initialize response.
const Version = "v1.0.0-dev"

// Server wraps mcp-go's server with a Tusk Runtime.
type Server struct {
	runtime *Runtime
	mcp     *server.MCPServer
}

// NewServer builds a Server, registers every Tusk tool, and returns it. The
// caller invokes ServeStdio or ServeSSE to start a transport.
func NewServer(runtime *Runtime) *Server {
	core := server.NewMCPServer(
		"tusk",
		Version,
		server.WithToolCapabilities(true),
	)

	srv := &Server{runtime: runtime, mcp: core}

	registerTools(srv)

	return srv
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

// MCP exposes the underlying mcp-go server (for advanced wiring/tests).
func (srv *Server) MCP() *server.MCPServer {
	return srv.mcp
}
```

- [ ] **Step 4: Stub `internal/mcp/tools.go` (registers nothing yet)**

```go
package mcp

// registerTools wires every Tusk tool onto srv.mcp. Real registrations land in
// later tasks; this skeleton lets the server compile.
func registerTools(srv *Server) {
	_ = srv
}
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/mcp/... -v
```

Expected: TestNewServer_ReturnsServer PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go internal/mcp/tools.go
git commit -m "feat(mcp): server skeleton wires mcp-go core + Runtime"
```

---

## Task 14: CLI — `tusk mcp` (stdio transport)

**Files:** Create `cmd/tusk/cmd_mcp.go`, `cmd/tusk/cmd_mcp_test.go`. Modify `cmd/tusk/root.go`.

- [ ] **Step 1: Write failing test**

`cmd/tusk/cmd_mcp_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestMCP_ParsesTransportFlag(test *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"mcp", "--help"})

	out := captureStdout(test, func() {
		_ = cmd.Execute()
	})

	if !strings.Contains(out, "--transport") {
		test.Errorf("expected --transport flag in help, got:\n%s", out)
	}

	if !strings.Contains(out, "stdio") || !strings.Contains(out, "sse") {
		test.Errorf("expected stdio and sse mentioned, got:\n%s", out)
	}
}

func TestMCP_RejectsUnknownTransport(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("mcp", "--transport", "bogus")

	if runErr == nil {
		test.Fatalf("expected error, got out:\n%s", out)
	}
}
```

`captureStdout` is a helper that exists alongside `runCLI`. If absent, mirror `cmd_init_test.go`'s pattern.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestMCP
```

Expected: FAIL — `mcp` subcommand missing.

- [ ] **Step 3: Write `cmd/tusk/cmd_mcp.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	var (
		transport string
		addr      string
	)

	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the long-running MCP server (stdio or SSE)",
		Long: `Run the Tusk MCP server.

Transports:
  stdio   reads JSON-RPC over stdin, writes over stdout (default)
  sse     listens for SSE clients on --addr (default :8765)

The server holds the workspace open for the lifetime of the session, drains
the embed queue in the background, and watches the workspace for external
edits.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			runtime, openErr := mcp.Open(cwd)

			if openErr != nil {
				return openErr
			}

			defer runtime.Close()

			server := mcp.NewServer(runtime)

			switch transport {
			case "stdio", "":
				return server.ServeStdio()
			case "sse":
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "tusk mcp: SSE listening on %s\n", addr)

				return server.ServeSSE(addr)
			}

			return fmt.Errorf("--transport: unknown value %q (want stdio|sse)", transport)
		},
	}

	mcpCmd.Flags().StringVar(&transport, "transport", "stdio", "transport: stdio | sse")
	mcpCmd.Flags().StringVar(&addr, "addr", ":8765", "SSE listen address (only used when --transport sse)")

	return mcpCmd
}
```

- [ ] **Step 4: Register in `cmd/tusk/root.go`**

```go
	rootCmd.AddCommand(newMCPCmd())
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestMCP -v
```

Expected: 2 PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_mcp.go cmd/tusk/cmd_mcp_test.go cmd/tusk/root.go
git commit -m "feat(cli): tusk mcp boots the long-running server (stdio default)"
```

---

## Module Conventions for MCP Tool Tasks (15–27)

Every tool task follows the same shape:

1. Add a failing handler test in `internal/mcp/tools_test.go` that calls the tool through `server.MCP()` (the embedded mcp-go server exposes `CallTool`).
2. Register the tool in `internal/mcp/tools.go` via `registerTools` — keep registrations alphabetized within each section (read tools first, then mutation, then long-running).
3. Implement the handler in the same file (group by section).
4. Re-run, verify pass, commit.

**Calling tools in tests:** mcp-go exposes a server-internal call helper. Since v0.52.0 doesn't expose a public `CallTool`, tests use `server.NewMCPServer(...)` plus a manual `mcp.CallToolRequest` and the registered handler closure (closures are stored alongside the registration; we expose `srv.handlerFor(name)` in tests via a `tools_helpers_test.go` file with `package mcp` access). The first MCP tool task (Task 15) lays this helper down.

**JSON encoding in tools:** every handler builds its result via `encoding/json.Marshal` and returns `mcp.NewToolResultText(string(jsonBytes))`. On error (validation, lock timeout, engine failure), return `mcp.NewToolResultError(err.Error())` — never propagate the raw `error` (mcp-go treats that as a transport-level fault).

**Argument parsing helpers:** add to `internal/mcp/tools.go` once, reuse across handlers:

```go
func argString(req mcp.CallToolRequest, key string) (string, error) {
	value, ok := req.Params.Arguments[key].(string)

	if !ok {
		return "", fmt.Errorf("missing or non-string argument %q", key)
	}

	return value, nil
}

func argStringOptional(req mcp.CallToolRequest, key string) string {
	value, _ := req.Params.Arguments[key].(string)

	return value
}

func argInt(req mcp.CallToolRequest, key string) (int, error) {
	raw, ok := req.Params.Arguments[key]

	if !ok {
		return 0, fmt.Errorf("missing argument %q", key)
	}

	switch typed := raw.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	}

	return 0, fmt.Errorf("argument %q is not a number", key)
}

func argIntOptional(req mcp.CallToolRequest, key string, defaultValue int) int {
	if value, parseErr := argInt(req, key); parseErr == nil {
		return value
	}

	return defaultValue
}

func argMap(req mcp.CallToolRequest, key string) map[string]any {
	value, _ := req.Params.Arguments[key].(map[string]any)

	return value
}

func argStringSlice(req mcp.CallToolRequest, key string) []string {
	raw, ok := req.Params.Arguments[key].([]any)

	if !ok {
		return nil
	}

	out := make([]string, 0, len(raw))

	for _, item := range raw {
		if str, isString := item.(string); isString {
			out = append(out, str)
		}
	}

	return out
}

func toolError(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}

func toolJSON(payload any) (*mcp.CallToolResult, error) {
	body, marshalErr := json.Marshal(payload)

	if marshalErr != nil {
		return nil, marshalErr
	}

	return mcp.NewToolResultText(string(body)), nil
}
```

Imports for `tools.go` (placeholder — actual imports grow as tools land):

```go
import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/status"
	"github.com/mark3labs/mcp-go/mcp"
)
```

---

## Task 15: Tool — `tusk_status` + tools test harness

**Files:** Create `internal/mcp/tools_test.go`. Modify `internal/mcp/tools.go`.

This task lays the test harness used by every following tool task.

- [ ] **Step 1: Write failing test**

`internal/mcp/tools_test.go`:

```go
package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// callTool is the harness every tool test uses. It runs the registered handler
// for `name` against `args` and returns the parsed JSON payload from the
// success result, or an error if the tool returned an MCP error.
func callTool(test *testing.T, srv *mcp.Server, name string, args map[string]any) (map[string]any, error) {
	test.Helper()

	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}

	result, callErr := srv.HandleToolCall(context.Background(), request)

	if callErr != nil {
		return nil, callErr
	}

	if result.IsError {
		return nil, fmtError(result)
	}

	if len(result.Content) == 0 {
		return map[string]any{}, nil
	}

	textContent, ok := result.Content[0].(mcpgo.TextContent)

	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var parsed map[string]any

	if unmarshalErr := json.Unmarshal([]byte(textContent.Text), &parsed); unmarshalErr != nil {
		test.Fatalf("unmarshal: %v\nbody: %s", unmarshalErr, textContent.Text)
	}

	return parsed, nil
}

func fmtError(result *mcpgo.CallToolResult) error {
	if len(result.Content) == 0 {
		return errMCP("(empty)")
	}

	if textContent, ok := result.Content[0].(mcpgo.TextContent); ok {
		return errMCP(textContent.Text)
	}

	return errMCP("(non-text error)")
}

type mcpError string

func (err mcpError) Error() string { return string(err) }

func errMCP(message string) error { return mcpError(message) }

func bootRuntime(test *testing.T) *mcp.Runtime {
	test.Helper()

	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return rt
}

func TestTool_Status(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", PropertiesJSON: "{}", LastChecksum: "x"})
	rt.Nodes.Upsert(index.NodeRow{ID: "notes/b", Type: "note", Path: "notes/b.md", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_status", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_status: %v", callErr)
	}

	counts, _ := body["nodes_by_type"].(map[string]any)

	if counts["ticket"].(float64) != 1 || counts["note"].(float64) != 1 {
		test.Errorf("counts = %v", counts)
	}
}
```

- [ ] **Step 2: Add `Server.HandleToolCall` helper**

In `internal/mcp/server.go`, append:

```go
// HandleToolCall is exported for tests; production code goes through stdio/SSE.
// It dispatches to the registered handler for request.Params.Name. Returns an
// "unknown tool" CallToolResult error when the tool isn't registered.
func (srv *Server) HandleToolCall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	handler, exists := srv.handlers[request.Params.Name]

	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("unknown tool %q", request.Params.Name)), nil
	}

	return handler(ctx, request)
}
```

Note: mcp-go v0.52.0's `server.MCPServer` keeps registered tools in an internal map — accessing them via reflection is brittle. We instead keep our own copy in `srv.handlers`. Update `Server`:

```go
type Server struct {
	runtime  *Runtime
	mcp      *server.MCPServer
	handlers map[string]server.ToolHandlerFunc
}
```

And in `NewServer`:

```go
	srv := &Server{
		runtime:  runtime,
		mcp:      core,
		handlers: map[string]server.ToolHandlerFunc{},
	}
```

Add a helper that registers in both places:

```go
// register adds tool to both the mcp-go server and srv.handlers.
func (srv *Server) register(tool mcp.Tool, handler server.ToolHandlerFunc) {
	srv.mcp.AddTool(tool, handler)
	srv.handlers[tool.Name] = handler
}
```

Imports needed in `server.go`: add `"context"`, `"fmt"`, and ensure `"github.com/mark3labs/mcp-go/mcp"` is imported.

- [ ] **Step 3: Register `tusk_status` in `internal/mcp/tools.go`**

Replace the placeholder body:

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/status"
	"github.com/mark3labs/mcp-go/mcp"
)

func registerTools(srv *Server) {
	registerStatusTool(srv)
}

func registerStatusTool(srv *Server) {
	tool := mcp.NewTool("tusk_status",
		mcp.WithDescription("Quick workspace summary: node counts by type, edge count, embed queue depth, last reindex time."),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		snap, snapErr := status.Snapshot(status.Config{
			Nodes:      srv.runtime.Nodes,
			Edges:      srv.runtime.Edges,
			EmbedQueue: srv.runtime.EmbedQueue,
			Meta:       srv.runtime.Meta,
		})

		if snapErr != nil {
			return toolError(snapErr), nil
		}

		return toolJSON(map[string]any{
			"nodes_by_type":     snap.NodesByType,
			"edge_count":        snap.EdgeCount,
			"embed_queue_depth": snap.EmbedQueueDepth,
			"last_reindex_at":   snap.LastReindexAt,
		})
	}

	srv.register(tool, handler)
}

// argString / argStringOptional / argInt / argIntOptional / argMap /
// argStringSlice / toolError / toolJSON helpers — see Module Conventions for
// MCP Tool Tasks above.

func argString(request mcp.CallToolRequest, key string) (string, error) {
	value, ok := request.Params.Arguments[key].(string)

	if !ok {
		return "", fmt.Errorf("missing or non-string argument %q", key)
	}

	return value, nil
}

func argStringOptional(request mcp.CallToolRequest, key string) string {
	value, _ := request.Params.Arguments[key].(string)

	return value
}

func argInt(request mcp.CallToolRequest, key string) (int, error) {
	raw, ok := request.Params.Arguments[key]

	if !ok {
		return 0, fmt.Errorf("missing argument %q", key)
	}

	switch typed := raw.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	}

	return 0, fmt.Errorf("argument %q is not a number", key)
}

func argIntOptional(request mcp.CallToolRequest, key string, defaultValue int) int {
	if value, parseErr := argInt(request, key); parseErr == nil {
		return value
	}

	return defaultValue
}

func argMap(request mcp.CallToolRequest, key string) map[string]any {
	value, _ := request.Params.Arguments[key].(map[string]any)

	return value
}

func argStringSlice(request mcp.CallToolRequest, key string) []string {
	raw, ok := request.Params.Arguments[key].([]any)

	if !ok {
		return nil
	}

	out := make([]string, 0, len(raw))

	for _, item := range raw {
		if str, isString := item.(string); isString {
			out = append(out, str)
		}
	}

	return out
}

func toolError(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}

func toolJSON(payload any) (*mcp.CallToolResult, error) {
	body, marshalErr := json.Marshal(payload)

	if marshalErr != nil {
		return nil, marshalErr
	}

	return mcp.NewToolResultText(string(body)), nil
}

var _ = index.NewNodeRepo // keep import alive while later tasks haven't wired more handlers
```

The two `var _ =` lines drop in later tasks as more handlers consume those imports.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/mcp/... -run TestTool_Status -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp
git commit -m "feat(mcp): tusk_status tool + tools test harness"
```

---

## Task 16: Tool — `tusk_node_get`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test to `tools_test.go`**

```go
func TestTool_NodeGet(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/hi.md",
		Type:    "note",
		Title:   "Hi there",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_get", map[string]any{"id": "notes/hi"})

	if callErr != nil {
		test.Fatalf("tusk_node_get: %v", callErr)
	}

	if body["id"] != "notes/hi" {
		test.Errorf("id = %v, want notes/hi", body["id"])
	}

	if body["type"] != "note" {
		test.Errorf("type = %v, want note", body["type"])
	}

	if body["title"] != "Hi there" {
		test.Errorf("title = %v, want 'Hi there'", body["title"])
	}
}
```

Add `"github.com/germanamz/tusk/internal/node"` to the imports.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/mcp/... -run TestTool_NodeGet
```

Expected: FAIL — tool unregistered.

- [ ] **Step 3: Register the tool in `tools.go`**

Add to `registerTools`:

```go
	registerNodeGetTool(srv)
```

Add the function:

```go
func registerNodeGetTool(srv *Server) {
	tool := mcp.NewTool("tusk_node_get",
		mcp.WithDescription("Read a node by id (workspace-relative path without extension). Returns id, type, path, title, properties, edges, body."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Node id (e.g. \"notes/hi\")")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		loaded, getErr := srv.runtime.NodeService.Get(nodeID)

		if getErr != nil {
			return toolError(getErr), nil
		}

		return toolJSON(map[string]any{
			"id":         loaded.ID,
			"type":       loaded.Type,
			"path":       loaded.Path,
			"title":      loaded.Title,
			"properties": loaded.Properties,
			"edges":      loaded.Edges,
			"body":       string(loaded.Body),
		})
	}

	srv.register(tool, handler)
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/mcp/... -run TestTool_NodeGet -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat(mcp): tusk_node_get tool"
```

---

## Task 17: Tool — `tusk_node_list`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_NodeList(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	for _, id := range []string{"notes/a", "notes/b"} {
		rt.Nodes.Upsert(index.NodeRow{ID: id, Type: "note", Path: id + ".md", PropertiesJSON: "{}", LastChecksum: "x"})
	}

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/x", Type: "ticket", Path: "tickets/x.md", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_list", map[string]any{"type": "note"})

	if callErr != nil {
		test.Fatalf("tusk_node_list: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 2 {
		test.Errorf("len(results) = %d, want 2", len(results))
	}
}
```

- [ ] **Step 2: Verify fail**

```bash
go test ./internal/mcp/... -run TestTool_NodeList
```

- [ ] **Step 3: Register in `tools.go`**

```go
	registerNodeListTool(srv)
```

Implementation:

```go
func registerNodeListTool(srv *Server) {
	tool := mcp.NewTool("tusk_node_list",
		mcp.WithDescription("List nodes from the index. Optional type filter narrows the result."),
		mcp.WithString("type", mcp.Description("Optional node type filter (e.g. \"ticket\"). Empty = all.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		typeFilter := argStringOptional(request, "type")

		nodes, listErr := srv.runtime.NodeService.List(node.ListFilter{Type: typeFilter})

		if listErr != nil {
			return toolError(listErr), nil
		}

		results := make([]map[string]any, 0, len(nodes))

		for _, item := range nodes {
			results = append(results, map[string]any{
				"id":    item.ID,
				"type":  item.Type,
				"path":  item.Path,
				"title": item.Title,
			})
		}

		return toolJSON(map[string]any{
			"results": results,
			"count":   len(results),
		})
	}

	srv.register(tool, handler)
}
```

Add `"github.com/germanamz/tusk/internal/node"` to `tools.go` imports if missing.

- [ ] **Step 4: Verify pass**

```bash
go test ./internal/mcp/... -run TestTool_NodeList -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/mcp
git commit -m "feat(mcp): tusk_node_list tool"
```

---

## Task 18: Tool — `tusk_edge_list`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_EdgeList(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Edges.UpsertAll("tickets/a", "tickets/a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", Ordinal: 0, SourcePath: "tickets/a.md"},
	})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_edge_list", map[string]any{"from": "tickets/a"})

	if callErr != nil {
		test.Fatalf("tusk_edge_list: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 1 {
		test.Fatalf("len(results) = %d, want 1", len(results))
	}

	first := results[0].(map[string]any)

	if first["type"] != "blocks" || first["target_id"] != "tickets/b" {
		test.Errorf("first = %v", first)
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register**

```go
	registerEdgeListTool(srv)
```

```go
func registerEdgeListTool(srv *Server) {
	tool := mcp.NewTool("tusk_edge_list",
		mcp.WithDescription("List edges. Provide from, to, or type to narrow."),
		mcp.WithString("from", mcp.Description("Source node id")),
		mcp.WithString("to", mcp.Description("Target node id")),
		mcp.WithString("type", mcp.Description("Edge type")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		from := argStringOptional(request, "from")
		to := argStringOptional(request, "to")
		edgeType := argStringOptional(request, "type")

		var rows []index.EdgeRow
		var listErr error

		switch {
		case from != "":
			rows, listErr = srv.runtime.Edges.ListBySource(from)
		case to != "":
			rows, listErr = srv.runtime.Edges.ListByTarget(to)
		case edgeType != "":
			rows, listErr = srv.runtime.Edges.ListByType(edgeType)
		default:
			rows, listErr = srv.runtime.Edges.ListAll()
		}

		if listErr != nil {
			return toolError(listErr), nil
		}

		results := make([]map[string]any, 0, len(rows))

		for _, row := range rows {
			results = append(results, map[string]any{
				"type":        row.Type,
				"source_id":   row.SourceID,
				"target_id":   row.TargetID,
				"ordinal":     row.Ordinal,
				"source_path": row.SourcePath,
			})
		}

		return toolJSON(map[string]any{"results": results, "count": len(results)})
	}

	srv.register(tool, handler)
}
```

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_EdgeList -v
git add internal/mcp
git commit -m "feat(mcp): tusk_edge_list tool"
```

---

## Task 19: Tool — `tusk_query`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_Query(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", Title: "Auth bug", PropertiesJSON: "{}", LastChecksum: "x"})
	rt.Nodes.Upsert(index.NodeRow{ID: "notes/x", Type: "note", Path: "notes/x.md", Title: "X", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{"filter": "type=ticket"})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 1 {
		test.Fatalf("len(results) = %d, want 1", len(results))
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register**

```go
	registerQueryTool(srv)
```

```go
func registerQueryTool(srv *Server) {
	tool := mcp.NewTool("tusk_query",
		mcp.WithDescription("Run a structural filter against the workspace, optionally ranked by semantic similarity."),
		mcp.WithString("filter", mcp.Required(), mcp.Description("Filter expression (e.g. 'type=ticket status=active')")),
		mcp.WithString("sort", mcp.Description("Sort spec (e.g. '+priority,-due')")),
		mcp.WithNumber("take", mcp.Description("Limit results to N rows")),
		mcp.WithNumber("skip", mcp.Description("Skip the first M rows (requires take)")),
		mcp.WithString("semantic", mcp.Description("Rank by cosine similarity to this query string")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filterText, parseErr := argString(request, "filter")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		sortSpec := argStringOptional(request, "sort")
		take := argIntOptional(request, "take", 0)
		skip := argIntOptional(request, "skip", 0)
		semanticQuery := argStringOptional(request, "semantic")

		expr, parseErrs := filter.NewParser(filterText).Parse()

		if len(parseErrs) > 0 {
			return toolError(parseErrs[0]), nil
		}

		if validateErrs := filter.Validate(expr, *srv.runtime.Manifest); len(validateErrs) > 0 {
			return toolError(validateErrs[0]), nil
		}

		sortKeys, sortErr := filter.ParseSort(sortSpec)

		if sortErr != nil {
			return toolError(sortErr), nil
		}

		sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{
			SortKeys: sortKeys,
			Take:     take,
			Skip:     skip,
		})

		if compileErr != nil {
			return toolError(compileErr), nil
		}

		rows, queryErr := srv.runtime.Index.DB().Query(sqlQuery, params...)

		if queryErr != nil {
			return toolError(queryErr), nil
		}

		defer rows.Close()

		type result struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Path  string `json:"path"`
			Title string `json:"title"`
		}

		var results []result
		var ids []string

		for rows.Next() {
			var (
				rowID, rowType, rowPath, rowTitle, propertiesRaw, lastChecksum string
				lastMtime, lastSize                                            int64
			)

			if scanErr := rows.Scan(&rowID, &rowType, &rowPath, &rowTitle, &propertiesRaw, &lastMtime, &lastSize, &lastChecksum); scanErr != nil {
				return toolError(scanErr), nil
			}

			results = append(results, result{ID: rowID, Type: rowType, Path: rowPath, Title: rowTitle})
			ids = append(ids, rowID)
		}

		if semanticQuery == "" {
			return toolJSON(map[string]any{"results": results, "count": len(results)})
		}

		if srv.runtime.Embedder == nil {
			return toolError(fmt.Errorf("semantic ranking requires [embeddings] in tusk.toml")), nil
		}

		queryVector, embedErr := srv.runtime.Embedder.Embed(ctx, []byte(semanticQuery))

		if embedErr != nil {
			return toolError(embedErr), nil
		}

		loaded, loadErr := srv.runtime.Embeddings.ListByNodeIDs(ids)

		if loadErr != nil {
			return toolError(loadErr), nil
		}

		candidates := make([]filter.SemanticCandidate, 0, len(loaded))

		for _, embeddingRow := range loaded {
			candidates = append(candidates, filter.SemanticCandidate{NodeID: embeddingRow.NodeID, Vector: embeddingRow.Vector})
		}

		ranked := filter.SemanticRank(candidates, queryVector)

		if take > 0 {
			startIdx := skip

			if startIdx > len(ranked) {
				startIdx = len(ranked)
			}

			endIdx := startIdx + take

			if endIdx > len(ranked) {
				endIdx = len(ranked)
			}

			ranked = ranked[startIdx:endIdx]
		}

		ranking := make([]map[string]any, 0, len(ranked))
		byID := map[string]result{}

		for _, item := range results {
			byID[item.ID] = item
		}

		for _, scored := range ranked {
			ranking = append(ranking, map[string]any{
				"id":    scored.NodeID,
				"score": scored.Score,
				"type":  byID[scored.NodeID].Type,
				"path":  byID[scored.NodeID].Path,
				"title": byID[scored.NodeID].Title,
			})
		}

		return toolJSON(map[string]any{
			"results": ranking,
			"count":   len(ranking),
			"model":   srv.runtime.Embedder.Model(),
		})
	}

	srv.register(tool, handler)
}
```

Add `"github.com/germanamz/tusk/internal/filter"` to imports.

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_Query -v
git add internal/mcp
git commit -m "feat(mcp): tusk_query tool (structural + optional semantic)"
```

---

## Task 20: Tool — `tusk_doctor`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_Doctor_CleanReport(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_doctor", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	issues, _ := body["issues"].([]any)

	if len(issues) != 0 {
		test.Errorf("expected 0 issues, got %d", len(issues))
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register**

```go
	registerDoctorTool(srv)
```

```go
func registerDoctorTool(srv *Server) {
	tool := mcp.NewTool("tusk_doctor",
		mcp.WithDescription("Surface validation warnings and index health issues (dangling edges, embed-queue retries)."),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		report, runErr := doctor.Run(doctor.Config{
			Nodes:      srv.runtime.Nodes,
			Edges:      srv.runtime.Edges,
			EmbedQueue: srv.runtime.EmbedQueue,
		})

		if runErr != nil {
			return toolError(runErr), nil
		}

		issues := make([]map[string]any, 0, len(report.Issues))

		for _, issue := range report.Issues {
			issues = append(issues, map[string]any{
				"kind":    issue.Kind,
				"node_id": issue.NodeID,
				"message": issue.Message,
			})
		}

		return toolJSON(map[string]any{
			"issues":            issues,
			"embed_queue_depth": report.EmbedQueueDepth,
		})
	}

	srv.register(tool, handler)
}
```

Add `"github.com/germanamz/tusk/internal/doctor"` to imports.

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_Doctor -v
git add internal/mcp
git commit -m "feat(mcp): tusk_doctor tool"
```

---

## Task 21: Tool — `tusk_node_create`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_NodeCreate(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_create", map[string]any{
		"path":  "notes/hello.md",
		"type":  "note",
		"title": "Hello",
		"body":  "World",
	})

	if callErr != nil {
		test.Fatalf("tusk_node_create: %v", callErr)
	}

	if body["id"] != "notes/hello" {
		test.Errorf("id = %v", body["id"])
	}

	row, getErr := rt.Nodes.Get("notes/hello")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if row.Title != "Hello" {
		test.Errorf("title = %q", row.Title)
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register**

```go
	registerNodeCreateTool(srv)
```

```go
func registerNodeCreateTool(srv *Server) {
	tool := mcp.NewTool("tusk_node_create",
		mcp.WithDescription("Create a new node file and index it. The path must be a workspace-relative path with extension."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Workspace-relative target path (e.g. notes/hello.md)")),
		mcp.WithString("type", mcp.Required(), mcp.Description("Node type")),
		mcp.WithString("title", mcp.Description("Optional title")),
		mcp.WithString("body", mcp.Description("Optional markdown body")),
		mcp.WithObject("properties", mcp.Description("Additional frontmatter properties")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, parseErr := argString(request, "path")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		nodeType, parseErr := argString(request, "type")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		title := argStringOptional(request, "title")
		body := argStringOptional(request, "body")
		properties := argMap(request, "properties")

		var created *node.Node

		lockErr := srv.runtime.WithWriteLock(func() error {
			out, createErr := srv.runtime.NodeService.Create(node.CreateInput{
				RelPath:    path,
				Type:       nodeType,
				Title:      title,
				Properties: properties,
				Body:       []byte(body),
			})

			if createErr != nil {
				return createErr
			}

			created = out

			return nil
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"id":    created.ID,
			"type":  created.Type,
			"path":  created.Path,
			"title": created.Title,
		})
	}

	srv.register(tool, handler)
}
```

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_NodeCreate -v
git add internal/mcp
git commit -m "feat(mcp): tusk_node_create tool (write-locked)"
```

---

## Task 22: Tool — `tusk_node_modify`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_NodeModify(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/x.md",
		Type:    "note",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_modify", map[string]any{
		"id":   "notes/x",
		"set":  map[string]any{"priority": float64(5)},
	})

	if callErr != nil {
		test.Fatalf("tusk_node_modify: %v", callErr)
	}

	if body["id"] != "notes/x" {
		test.Errorf("id = %v", body["id"])
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register**

```go
	registerNodeModifyTool(srv)
```

```go
func registerNodeModifyTool(srv *Server) {
	tool := mcp.NewTool("tusk_node_modify",
		mcp.WithDescription("Modify a node's frontmatter properties. Cannot change type."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Node id")),
		mcp.WithObject("set", mcp.Description("Properties to upsert (key→value)")),
		mcp.WithArray("unset", mcp.Description("Property keys to remove"), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithString("body", mcp.Description("Optional new markdown body")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		setProps := argMap(request, "set")
		unsetKeys := argStringSlice(request, "unset")

		input := node.ModifyInput{
			ID:        nodeID,
			SetProps:  setProps,
			UnsetKeys: unsetKeys,
		}

		if rawBody, hasBody := request.Params.Arguments["body"].(string); hasBody {
			body := []byte(rawBody)
			input.Body = &body
		}

		var modified *node.Node

		lockErr := srv.runtime.WithWriteLock(func() error {
			out, modifyErr := srv.runtime.NodeService.Modify(input)

			if modifyErr != nil {
				return modifyErr
			}

			modified = out

			return nil
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"id":         modified.ID,
			"type":       modified.Type,
			"path":       modified.Path,
			"title":      modified.Title,
			"properties": modified.Properties,
		})
	}

	srv.register(tool, handler)
}
```

Note: `mcp.WithArray` and `mcp.Items` are mcp-go v0.52.0 helpers. If signatures differ in your installed version, consult `go doc github.com/mark3labs/mcp-go/mcp WithArray` and adjust.

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_NodeModify -v
git add internal/mcp
git commit -m "feat(mcp): tusk_node_modify tool"
```

---

## Task 23: Tool — `tusk_node_move`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_NodeMove(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/old.md",
		Type:    "note",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_move", map[string]any{
		"id":       "notes/old",
		"new_path": "notes/new.md",
	})

	if callErr != nil {
		test.Fatalf("tusk_node_move: %v", callErr)
	}

	if body["new_id"] != "notes/new" {
		test.Errorf("new_id = %v", body["new_id"])
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register**

```go
	registerNodeMoveTool(srv)
```

```go
func registerNodeMoveTool(srv *Server) {
	tool := mcp.NewTool("tusk_node_move",
		mcp.WithDescription("Atomically rename a node and rewrite incoming edges."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Current node id")),
		mcp.WithString("new_path", mcp.Required(), mcp.Description("New workspace-relative path with extension")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		newPath, parseErr := argString(request, "new_path")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		var plan *node.RenamePlan

		lockErr := srv.runtime.WithWriteLock(func() error {
			out, renameErr := node.Rename(srv.runtime.Root, srv.runtime.Nodes, srv.runtime.Edges, srv.runtime.Manifest.EdgeTypes, nodeID, newPath)

			if renameErr != nil {
				return renameErr
			}

			plan = out

			return nil
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"old_id":         plan.OldID,
			"new_id":         plan.NewID,
			"old_path":       plan.OldPath,
			"new_path":       plan.NewPath,
			"affected_files": plan.AffectedFiles,
		})
	}

	srv.register(tool, handler)
}
```

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_NodeMove -v
git add internal/mcp
git commit -m "feat(mcp): tusk_node_move tool"
```

---

## Task 24: Tool — `tusk_node_delete`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_NodeDelete(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/del.md",
		Type:    "note",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	if _, callErr := callTool(test, srv, "tusk_node_delete", map[string]any{"id": "notes/del"}); callErr != nil {
		test.Fatalf("tusk_node_delete: %v", callErr)
	}

	if _, getErr := rt.Nodes.Get("notes/del"); getErr == nil {
		test.Errorf("expected Get error after delete")
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register**

```go
	registerNodeDeleteTool(srv)
```

```go
func registerNodeDeleteTool(srv *Server) {
	tool := mcp.NewTool("tusk_node_delete",
		mcp.WithDescription("Remove a node file and its outgoing edges; incoming edges become dangling."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Node id to delete")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		lockErr := srv.runtime.WithWriteLock(func() error {
			return node.Delete(srv.runtime.Root, srv.runtime.Nodes, srv.runtime.Edges, nodeID)
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{"deleted_id": nodeID})
	}

	srv.register(tool, handler)
}
```

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_NodeDelete -v
git add internal/mcp
git commit -m "feat(mcp): tusk_node_delete tool"
```

---

## Task 25: Tools — `tusk_edge_add` and `tusk_edge_remove`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing tests**

```go
func TestTool_EdgeAddRemove(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", PropertiesJSON: "{}", LastChecksum: "x"})
	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/b", Type: "ticket", Path: "tickets/b.md", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	if _, callErr := callTool(test, srv, "tusk_edge_add", map[string]any{
		"type":      "blocks",
		"source_id": "tickets/a",
		"target_id": "tickets/b",
	}); callErr != nil {
		test.Fatalf("tusk_edge_add: %v", callErr)
	}

	rows, _ := rt.Edges.ListBySource("tickets/a")

	if len(rows) != 1 {
		test.Fatalf("len(rows) = %d after add, want 1", len(rows))
	}

	if _, callErr := callTool(test, srv, "tusk_edge_remove", map[string]any{
		"type":      "blocks",
		"source_id": "tickets/a",
		"target_id": "tickets/b",
	}); callErr != nil {
		test.Fatalf("tusk_edge_remove: %v", callErr)
	}

	rows, _ = rt.Edges.ListBySource("tickets/a")

	if len(rows) != 0 {
		test.Errorf("expected 0 rows after remove, got %d", len(rows))
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register both tools**

```go
	registerEdgeAddTool(srv)
	registerEdgeRemoveTool(srv)
```

Background: the CLI in `cmd/tusk/cmd_edge_add.go` / `cmd_edge_remove.go` writes edges with a synthetic `source_path = "__cli__"` (constant `cliSourcePath` in `cmd_edge.go`). MCP-added edges follow the same pattern with `source_path = "__mcp__"` so the watcher's full-tree reindex doesn't blow them away.

Add a constant in `internal/mcp/tools.go`:

```go
// mcpSourcePath is the synthetic source_path attributed to edges added via MCP
// tools. Mirrors cmd/tusk's cliSourcePath; both keep MCP/CLI-added edges
// distinguishable from edges discovered in node frontmatter.
const mcpSourcePath = "__mcp__"
```

Implementations:

```go
func registerEdgeAddTool(srv *Server) {
	tool := mcp.NewTool("tusk_edge_add",
		mcp.WithDescription("Add a typed edge from source_id to target_id."),
		mcp.WithString("type", mcp.Required()),
		mcp.WithString("source_id", mcp.Required()),
		mcp.WithString("target_id", mcp.Required()),
		mcp.WithNumber("ordinal", mcp.Description("Optional ordinal (default appends)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		edgeType, parseErr := argString(request, "type")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		sourceID, parseErr := argString(request, "source_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		targetID, parseErr := argString(request, "target_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		edgeDef, declared := srv.runtime.Manifest.EdgeTypes[edgeType]

		if !declared {
			return toolError(fmt.Errorf("edge type %q not declared in manifest", edgeType)), nil
		}

		lockErr := srv.runtime.WithWriteLock(func() error {
			sourceRow, sourceErr := srv.runtime.Nodes.Get(sourceID)

			if sourceErr != nil {
				return fmt.Errorf("source: %w", sourceErr)
			}

			if !edgeDef.AllowsSource(sourceRow.Type) {
				return fmt.Errorf("edge type %q does not allow source type %q", edgeType, sourceRow.Type)
			}

			if targetRow, getErr := srv.runtime.Nodes.Get(targetID); getErr == nil {
				if !edgeDef.AllowsTarget(targetRow.Type) {
					return fmt.Errorf("edge type %q does not allow target type %q", edgeType, targetRow.Type)
				}
			}

			if edgeDef.Acyclic {
				existing, listErr := srv.runtime.Edges.ListByType(edgeType)

				if listErr != nil {
					return listErr
				}

				adjacency := map[string][]string{}

				for _, row := range existing {
					adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
				}

				if cycleErr := node.DetectCycle(node.CycleProbe{EdgeType: edgeType, Source: sourceID, Target: targetID}, adjacency); cycleErr != nil {
					return cycleErr
				}
			}

			existingForSource, listErr := srv.runtime.Edges.ListBySource(sourceID)

			if listErr != nil {
				return listErr
			}

			var mcpEdges []index.EdgeRow

			for _, row := range existingForSource {
				if row.SourcePath == mcpSourcePath {
					mcpEdges = append(mcpEdges, row)
				}
			}

			ordinal := -1

			for _, row := range mcpEdges {
				if row.Type == edgeType && row.Ordinal > ordinal {
					ordinal = row.Ordinal
				}
			}

			ordinal++

			mcpEdges = append(mcpEdges, index.EdgeRow{
				Type:       edgeType,
				SourceID:   sourceID,
				TargetID:   targetID,
				Ordinal:    ordinal,
				SourcePath: mcpSourcePath,
			})

			return srv.runtime.Edges.UpsertAll(sourceID, mcpSourcePath, mcpEdges)
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"type":      edgeType,
			"source_id": sourceID,
			"target_id": targetID,
		})
	}

	srv.register(tool, handler)
}

func registerEdgeRemoveTool(srv *Server) {
	tool := mcp.NewTool("tusk_edge_remove",
		mcp.WithDescription("Remove a typed edge from source_id to target_id."),
		mcp.WithString("type", mcp.Required()),
		mcp.WithString("source_id", mcp.Required()),
		mcp.WithString("target_id", mcp.Required()),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		edgeType, parseErr := argString(request, "type")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		sourceID, parseErr := argString(request, "source_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		targetID, parseErr := argString(request, "target_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		lockErr := srv.runtime.WithWriteLock(func() error {
			rows, listErr := srv.runtime.Edges.ListBySource(sourceID)

			if listErr != nil {
				return listErr
			}

			var kept []index.EdgeRow
			removed := 0

			for _, row := range rows {
				if row.SourcePath != mcpSourcePath {
					continue
				}

				if row.Type == edgeType && row.TargetID == targetID {
					removed++

					continue
				}

				kept = append(kept, row)
			}

			if removed == 0 {
				return fmt.Errorf("no MCP-added edge matches type=%q source=%q target=%q", edgeType, sourceID, targetID)
			}

			counters := map[string]int{}

			for idx := range kept {
				kept[idx].Ordinal = counters[kept[idx].Type]
				counters[kept[idx].Type]++
			}

			return srv.runtime.Edges.UpsertAll(sourceID, mcpSourcePath, kept)
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"type":      edgeType,
			"source_id": sourceID,
			"target_id": targetID,
			"removed":   true,
		})
	}

	srv.register(tool, handler)
}
```

Add `"github.com/germanamz/tusk/internal/node"` to imports if missing (the cycle check uses it).

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_EdgeAddRemove -v
git add internal/mcp
git commit -m "feat(mcp): tusk_edge_add and tusk_edge_remove tools"
```

---

## Task 26: Tool — `tusk_reindex`

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestTool_Reindex(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if writeErr := os.WriteFile(filepath.Join(rt.Root, "notes/x.md"),
		[]byte("---\ntype: note\ntitle: x\n---\n\nbody\n"), 0o644); writeErr != nil {
		_ = os.MkdirAll(filepath.Join(rt.Root, "notes"), 0o755)
		os.WriteFile(filepath.Join(rt.Root, "notes/x.md"), []byte("---\ntype: note\ntitle: x\n---\n\nbody\n"), 0o644)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_reindex", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_reindex: %v", callErr)
	}

	if body["indexed"].(float64) < 1 {
		test.Errorf("expected indexed >= 1, got %v", body["indexed"])
	}
}
```

- [ ] **Step 2: Verify fail**

- [ ] **Step 3: Register**

```go
	registerReindexTool(srv)
```

```go
func registerReindexTool(srv *Server) {
	tool := mcp.NewTool("tusk_reindex",
		mcp.WithDescription("Walk the workspace and bring the index up to date with disk."),
		mcp.WithBoolean("no_embed", mcp.Description("Skip the embedding pass")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		noEmbed, _ := request.Params.Arguments["no_embed"].(bool)

		var report *reindex.Report

		lockErr := srv.runtime.WithWriteLock(func() error {
			config := reindex.Config{
				Root:            srv.runtime.Root,
				Repo:            srv.runtime.Nodes,
				Edges:           srv.runtime.Edges,
				EdgeTypes:       srv.runtime.Manifest.EdgeTypes,
				WorkspaceIgnore: srv.runtime.Manifest.Workspace.Ignore,
				Meta:            srv.runtime.Meta,
			}

			if !noEmbed && srv.runtime.Embedder != nil {
				config.EmbedQueue = srv.runtime.EmbedQueue
				config.EmbeddingRepo = srv.runtime.Embeddings
				config.Embedder = srv.runtime.Embedder
				config.Chunker = srv.runtime.Chunker
			}

			out, runErr := reindex.Run(config)

			if runErr != nil {
				return runErr
			}

			report = out

			return nil
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"indexed": report.Indexed,
			"removed": report.Removed,
			"skipped": report.Skipped,
		})
	}

	srv.register(tool, handler)
}
```

Add `"github.com/germanamz/tusk/internal/reindex"` to imports if not present.

- [ ] **Step 4: Verify pass; commit**

```bash
go test ./internal/mcp/... -run TestTool_Reindex -v
git add internal/mcp
git commit -m "feat(mcp): tusk_reindex tool"
```

---

## Task 27: Background embed-queue drainer

**Files:** Create `internal/mcp/drainer.go`, `internal/mcp/drainer_test.go`.

- [ ] **Step 1: Write failing test**

`internal/mcp/drainer_test.go`:

```go
package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
)

func TestRunDrainer_DrainsQueueAndStopsOnCancel(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "notes/x", Type: "note", Path: "notes/x.md", PropertiesJSON: "{}", LastChecksum: "x"})

	// No embedder → drainer is a no-op but should still respect the ticker
	// and exit on context cancellation cleanly.
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- mcp.RunDrainer(ctx, mcp.DrainerConfig{
			Runtime:  rt,
			Interval: 20 * time.Millisecond,
		})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case runErr := <-done:
		if runErr != nil {
			test.Fatalf("RunDrainer returned %v", runErr)
		}
	case <-time.After(2 * time.Second):
		test.Fatalf("RunDrainer did not exit after cancel")
	}
}
```

- [ ] **Step 2: Verify fail**

```bash
go test ./internal/mcp/... -run TestRunDrainer
```

Expected: FAIL — `RunDrainer` undefined.

- [ ] **Step 3: Write `internal/mcp/drainer.go`**

```go
package mcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/germanamz/tusk/internal/embed"
)

// DrainerConfig configures RunDrainer.
type DrainerConfig struct {
	Runtime  *Runtime
	Interval time.Duration // default 2 * time.Second
	Logger   *slog.Logger  // optional; nil silences output
}

// RunDrainer loops on a ticker calling embed.DrainQueue until ctx cancels. When
// the runtime has no embedder configured, RunDrainer is a no-op but still
// respects ctx cancellation.
func RunDrainer(ctx context.Context, config DrainerConfig) error {
	interval := config.Interval

	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if config.Runtime.Embedder == nil {
				continue
			}

			drained, drainErr := embed.DrainQueue(ctx, embed.DrainConfig{
				Root:       config.Runtime.Root,
				Nodes:      config.Runtime.Nodes,
				Queue:      config.Runtime.EmbedQueue,
				Embeddings: config.Runtime.Embeddings,
				Embedder:   config.Runtime.Embedder,
				Chunker:    config.Runtime.Chunker,
			})

			if drainErr != nil && config.Logger != nil {
				config.Logger.Warn("drainer error", "err", drainErr)
			}

			if drained > 0 && config.Logger != nil {
				config.Logger.Info("drainer batch", "count", drained)
			}
		}
	}
}
```

- [ ] **Step 4: Verify pass**

```bash
go test ./internal/mcp/... -run TestRunDrainer -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/drainer.go internal/mcp/drainer_test.go
git commit -m "feat(mcp): background embed-queue drainer"
```

---

## Task 28: Watcher integration

**Files:** Create `internal/mcp/watch.go`, `internal/mcp/watch_test.go`.

- [ ] **Step 1: Write failing test**

`internal/mcp/watch_test.go`:

```go
package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/mcp"
)

func TestRunWatcher_PicksUpExternalEdit(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- mcp.RunWatcher(ctx, mcp.WatchConfig{Runtime: rt})
	}()

	time.Sleep(100 * time.Millisecond) // let watcher boot

	if mkErr := os.MkdirAll(filepath.Join(rt.Root, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	body := []byte("---\ntype: note\ntitle: external\n---\n\nbody\n")

	if writeErr := os.WriteFile(filepath.Join(rt.Root, "notes/external.md"), body, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if _, getErr := rt.Nodes.Get("notes/external"); getErr == nil {
			cancel()
			<-done

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	<-done

	test.Fatalf("expected node notes/external to be indexed after watcher saw the write")
}
```

- [ ] **Step 2: Verify fail**

```bash
go test ./internal/mcp/... -run TestRunWatcher
```

- [ ] **Step 3: Write `internal/mcp/watch.go`**

```go
package mcp

import (
	"context"
	"log/slog"

	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/watcher"
)

// WatchConfig configures RunWatcher.
type WatchConfig struct {
	Runtime *Runtime
	Logger  *slog.Logger
}

// RunWatcher boots an fsnotify watcher rooted at runtime.Root and reacts to
// every debounced event by re-running the full reindex pass. Plan 6 mirrors
// Plan 3's full-tree reindex strategy; single-file partial reindex lands in
// Plan 8.
func RunWatcher(ctx context.Context, config WatchConfig) error {
	instance, newErr := watcher.New(config.Runtime.Root)

	if newErr != nil {
		return newErr
	}

	defer instance.Close()

	handler := func(event watcher.WatchEvent) error {
		if event.Path == "" || event.Path == "." {
			return nil
		}

		lockErr := config.Runtime.WithWriteLock(func() error {
			_, runErr := reindex.Run(reindex.Config{
				Root:            config.Runtime.Root,
				Repo:            config.Runtime.Nodes,
				Edges:           config.Runtime.Edges,
				EdgeTypes:       config.Runtime.Manifest.EdgeTypes,
				WorkspaceIgnore: config.Runtime.Manifest.Workspace.Ignore,
				EmbedQueue:      config.Runtime.EmbedQueue,
				EmbeddingRepo:   config.Runtime.Embeddings,
				Embedder:        config.Runtime.Embedder,
				Chunker:         config.Runtime.Chunker,
				Meta:            config.Runtime.Meta,
			})

			return runErr
		})

		if lockErr != nil && config.Logger != nil {
			config.Logger.Warn("watcher reindex error", "err", lockErr, "path", event.Path)
		}

		return nil
	}

	return instance.Run(ctx, handler)
}
```

- [ ] **Step 4: Verify pass**

```bash
go test ./internal/mcp/... -run TestRunWatcher -v
```

Expected: PASS within 5s.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/watch.go internal/mcp/watch_test.go
git commit -m "feat(mcp): fsnotify watcher integration for tusk mcp"
```

---

## Task 29: Wire drainer + watcher into the MCP server lifecycle

**Files:** Modify `internal/mcp/server.go`, `cmd/tusk/cmd_mcp.go`.

The drainer and watcher exist in isolation — Task 29 launches them alongside the transport so a single `tusk mcp` invocation runs the full long-running mode.

- [ ] **Step 1: Append `Server.RunBackground`**

In `internal/mcp/server.go`:

```go
import (
	"context"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RunBackground starts the embed-queue drainer and the file watcher. It blocks
// until ctx cancels, then returns the first non-nil error from either worker.
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

	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()
		record(RunDrainer(ctx, DrainerConfig{Runtime: srv.runtime}))
	}()

	go func() {
		defer waitGroup.Done()
		record(RunWatcher(ctx, WatchConfig{Runtime: srv.runtime}))
	}()

	waitGroup.Wait()

	return first
}
```

- [ ] **Step 2: Update `cmd/tusk/cmd_mcp.go` to spawn background workers**

Replace the body of `RunE` (transport switch) with:

```go
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		serverInstance := mcp.NewServer(runtime)

		bgDone := make(chan error, 1)

		go func() {
			bgDone <- serverInstance.RunBackground(ctx)
		}()

		var transportErr error

		switch transport {
		case "stdio", "":
			transportErr = serverInstance.ServeStdio()
		case "sse":
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "tusk mcp: SSE listening on %s\n", addr)
			transportErr = serverInstance.ServeSSE(addr)
		default:
			cancel()
			<-bgDone

			return fmt.Errorf("--transport: unknown value %q (want stdio|sse)", transport)
		}

		cancel()

		<-bgDone

		return transportErr
```

Add the imports: `"context"`, `"os/signal"`, `"syscall"` (and keep `"os"`, `"fmt"`, `"github.com/germanamz/tusk/internal/mcp"`).

- [ ] **Step 3: Run, build, smoke**

```bash
go build ./...
make test
```

Expected: green.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/server.go cmd/tusk/cmd_mcp.go
git commit -m "feat(mcp): tusk mcp runs drainer + watcher alongside transport"
```

---

## Task 30: SSE transport smoke test

**Files:** Modify `cmd/tusk/cmd_mcp_test.go` (or add a new `cmd_mcp_sse_test.go`).

- [ ] **Step 1: Append failing test**

```go
func TestMCP_SSEStartsListener(test *testing.T) {
	root := setupTempWorkspace(test)
	chdir(test, root)
	defer chdir(test, "")

	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")

	if listenErr != nil {
		test.Fatalf("listen: %v", listenErr)
	}

	addr := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/tusk", "mcp", "--transport", "sse", "--addr", addr)
	cmd.Dir = repoRoot(test)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	_ = stdout
	_ = stderr

	if startErr := cmd.Start(); startErr != nil {
		test.Fatalf("start: %v", startErr)
	}

	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond); dialErr == nil {
			conn.Close()

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	test.Fatalf("SSE server never accepted on %s", addr)
}

// repoRoot walks up from the cwd to find the tusk repo root (containing go.mod).
func repoRoot(test *testing.T) string {
	test.Helper()

	dir, _ := os.Getwd()

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)

		if parent == dir {
			test.Fatalf("could not find repo root from %s", dir)
		}

		dir = parent
	}
}
```

Add imports: `"context"`, `"net"`, `"os"`, `"os/exec"`, `"path/filepath"`, `"time"`.

- [ ] **Step 2: Verify fail (or skip if SSE wiring already exposes the listener)**

```bash
go test ./cmd/tusk/... -run TestMCP_SSEStartsListener -v
```

If the test fails because mcp-go's `SSEServer.Start` errors when given a port, instead launch via `cmd_mcp.go`'s flow with a 1s timeout context — the test only needs to confirm the process listens.

- [ ] **Step 3: Adjust if needed and verify pass**

If mcp-go's SSE server requires Address, not full host:port, normalize before passing. Check `go doc github.com/mark3labs/mcp-go/server NewSSEServer` and `Start`.

- [ ] **Step 4: Commit**

```bash
git add cmd/tusk
git commit -m "test(cli): smoke test for tusk mcp --transport sse"
```

---

## Task 31: End-to-end MCP session test

**Files:** Create `cmd/tusk/e2e_mcp_test.go`.

This test spawns `tusk mcp --transport stdio`, drives it via stdin/stdout JSON-RPC, exercises a full create → list → query → delete cycle, and verifies the watcher + drainer don't break the response flow.

- [ ] **Step 1: Write failing test**

`cmd/tusk/e2e_mcp_test.go`:

```go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// e2eRequest is a tiny JSON-RPC client driving stdin/stdout of `tusk mcp`.
type e2eClient struct {
	cmd    *exec.Cmd
	stdin  io.Writer
	stdout *bufio.Reader
}

func (client *e2eClient) call(method string, params map[string]any) (map[string]any, error) {
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  method,
		"params":  params,
	}

	body, _ := json.Marshal(request)

	if _, writeErr := client.stdin.Write(append(body, '\n')); writeErr != nil {
		return nil, writeErr
	}

	line, readErr := client.stdout.ReadBytes('\n')

	if readErr != nil {
		return nil, readErr
	}

	var response map[string]any

	if unmarshalErr := json.Unmarshal(line, &response); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return response, nil
}

func TestE2E_MCPStdioSession(test *testing.T) {
	root := setupTempWorkspace(test)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/tusk", "mcp")
	cmd.Dir = repoRoot(test)
	cmd.Env = append(cmd.Environ(), "PWD="+root)

	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()

	cmd.Dir = root // run with workspace as cwd

	if startErr := cmd.Start(); startErr != nil {
		test.Fatalf("start: %v", startErr)
	}

	defer func() {
		stdin.Close()
		_ = cmd.Wait()
	}()

	client := &e2eClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}

	// Step 1: initialize
	if _, callErr := client.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "tusk-e2e", "version": "0.0.1"},
	}); callErr != nil {
		test.Fatalf("initialize: %v", callErr)
	}

	// Step 2: list tools
	listResponse, listErr := client.call("tools/list", map[string]any{})

	if listErr != nil {
		test.Fatalf("tools/list: %v", listErr)
	}

	resultMap, _ := listResponse["result"].(map[string]any)
	tools, _ := resultMap["tools"].([]any)

	if len(tools) < 10 {
		test.Errorf("expected >=10 tools registered, got %d", len(tools))
	}

	// Step 3: tusk_node_create
	if _, callErr := client.call("tools/call", map[string]any{
		"name": "tusk_node_create",
		"arguments": map[string]any{
			"path":  "notes/e2e.md",
			"type":  "note",
			"title": "E2E",
		},
	}); callErr != nil {
		test.Fatalf("tusk_node_create: %v", callErr)
	}

	// Step 4: tusk_query → expect 1 result
	queryResponse, queryErr := client.call("tools/call", map[string]any{
		"name": "tusk_query",
		"arguments": map[string]any{
			"filter": "type=note",
		},
	})

	if queryErr != nil {
		test.Fatalf("tusk_query: %v", queryErr)
	}

	queryResult, _ := queryResponse["result"].(map[string]any)
	contents, _ := queryResult["content"].([]any)

	if len(contents) == 0 {
		test.Fatalf("query returned no content")
	}

	textBody := contents[0].(map[string]any)["text"].(string)

	var queryBody map[string]any

	if unmarshalErr := json.Unmarshal([]byte(textBody), &queryBody); unmarshalErr != nil {
		test.Fatalf("unmarshal query body: %v", unmarshalErr)
	}

	if int(queryBody["count"].(float64)) != 1 {
		test.Errorf("count = %v, want 1", queryBody["count"])
	}

	// Step 5: ensure the file is on disk (verifies workspace lock didn't deadlock)
	if _, statErr := stat(filepath.Join(root, "notes", "e2e.md")); statErr != nil {
		test.Errorf("notes/e2e.md not on disk: %v", statErr)
	}
}

func stat(path string) (any, error) {
	return filepath.Abs(path) // dummy — replace with os.Stat at implementation time
}
```

(The `stat` placeholder in the test exists so the file compiles before implementation; replace with `os.Stat` and add the `os` import in Step 3.)

- [ ] **Step 2: Run, verify the harness**

```bash
go test ./cmd/tusk/... -run TestE2E_MCPStdioSession -v -count=1
```

Expected: FAIL — likely on JSON-RPC framing if mcp-go expects Content-Length headers in stdio. Read mcp-go's stdio docs (`go doc github.com/mark3labs/mcp-go/server ServeStdio`) and adjust the read loop accordingly. mcp-go v0.52.0 uses newline-delimited JSON over stdio (no Content-Length headers); the harness above matches that.

- [ ] **Step 3: Replace the `stat` placeholder**

```go
import "os"

func stat(path string) (any, error) {
	return os.Stat(path)
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestE2E_MCPStdioSession -v -count=1
```

Expected: PASS. If the goroutines from drainer/watcher cause flakes, raise the `cancel()` window and re-run.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/e2e_mcp_test.go
git commit -m "test(cli): e2e tusk mcp stdio session covers initialize/list/create/query"
```

---

## Task 32: Final smoke + doc touch-up

- [ ] **Step 1: Run full test suite**

```bash
make test
make vet
```

Expected: every package green.

- [ ] **Step 2: Build the binary and smoke tools**

```bash
make build
./bin/tusk --help | grep -E "(mcp|doctor|status)"
```

Expected: `tusk mcp`, `tusk doctor`, `tusk status` all listed.

- [ ] **Step 3: Touch up `CLAUDE.md` if needed**

If the project's `CLAUDE.md` mentions "v1 features ship", confirm Plan 6 is reflected — but only as a status update, not as a feature list. Keep changes minimal.

- [ ] **Step 4: Final commit (only if there are uncommitted doc edits)**

```bash
git status --short

# If non-empty:
git add -p   # interactively stage what should ship
git commit -m "docs(plan-6): note MCP server + missing CLI verbs"
```

- [ ] **Step 5: Summary**

```bash
git log --oneline v1..feat/plan-6
```

Expected: ~25–32 commits, one per task (some tasks split across multiple commits).

---

## Self-Review Notes

**Spec coverage:**
- §11.3 MCP surface — every tool listed in the spec is registered (Task 15–26), except `tusk_init` which is documented as deferred in Excluded for Plan 6. tasks include doctor and status tools which have CLI parity (Task 7–10).
- §9.2 file watcher — Task 28 (`internal/mcp/watch.go`).
- §9.8 concurrency — every mutation tool wraps `WithWriteLock`; reads bypass.
- §10.6 async embedding pipeline — Task 27 (background drainer); Task 4 extracts the shared `embed.DrainQueue`.
- §11.4 output modes — every tool returns a single JSON document (matches `--json` mode); textual output stays in CLI.

**Decisions locked in:**
- mcp-go v0.52.0 — single new dependency.
- `meta(key, value)` table for `last_reindex_at` — Task 1.
- Per-write workspace lock (not session-wide) — `Runtime.WithWriteLock`.
- Drainer tick interval default 2s; watcher debounce 500ms (inherited from `internal/watcher`).
- SSE default address `:8765`.

**Type consistency:** All `mcp.Tool` and `mcp.CallToolRequest` types come from `github.com/mark3labs/mcp-go/mcp`; `server.MCPServer` and `server.ToolHandlerFunc` come from `.../server`. The tests reference both via `mcpgo` alias.

**Followups (post-Plan-6):**
- Pack-aware MCP tools (`tusk_ticket_open` etc.) when type packs ship.
- Single-file partial reindex from watcher events (Plan 8 polish).
- `tusk_init` MCP tool — needs runtime hot-reload semantics.
