# Phase 1: Store & Migration Infrastructure — SQLite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the foundational SQLite infrastructure — embedded migrations, the Store struct that opens/configures/migrates the database, and shared helper functions — so that all future repository implementations (tasks, projects, tags, etc.) have a working database and test harness to build on.

**Prerequisites:** None — this is the first phase.

**What you'll learn:** `go:embed`, `database/sql` basics, SQLite pragmas, migration patterns, Go test helpers.

**Estimated effort:** 1-2 hours.

---

## Context

### What is Tusk?

Tusk is a concurrent-safe task management tool written in Go. It ships as a single binary with SQLite persistence and exposes every capability through both a CLI and an MCP (Model Context Protocol) server, so AI agents can manage tasks alongside humans. Think of it as TaskWarrior's speed combined with Linear's structure and Jira's workflows — without the bloat.

### Architecture Overview

Tusk follows a layered architecture where each layer only talks to the one below it:

```
CLI / MCP Server   (user-facing interfaces)
       │
   Service Layer   (business logic, validation, orchestration)
       │
  Repository Layer (Go interfaces — e.g., TaskRepository, ProjectRepository)
       │
   Storage Layer   (concrete implementations — SQLite, future: PostgreSQL, etc.)
```

The **Repository Layer** defines Go interfaces (already implemented in `internal/repository/`). The **Storage Layer** provides concrete structs that satisfy those interfaces. This phase builds the foundation of the Storage Layer: the `Store` struct that manages the SQLite connection and runs migrations. Future phases will add the actual repository structs (`TaskRepo`, `ProjectRepo`, etc.) on top of this foundation.

### Project Structure (relevant files)

```
tusk/
├── go.mod                              # Go module: github.com/germanamz/tusk
├── go.sum
├── migrations/
│   ├── 001_initial.up.sql              # EXISTS — the DDL that creates all tables
│   ├── 001_initial.down.sql            # EXISTS — the rollback SQL
│   └── migrations.go                   # TO CREATE — embeds .sql files into the binary
├── internal/
│   ├── domain/                         # EXISTS — domain types (Task, Project, etc.)
│   ├── repository/                     # EXISTS — repository interfaces
│   └── sqlite/
│       ├── store.go                    # EXISTS as stub — TO REPLACE with full implementation
│       ├── store_test.go              # TO CREATE — tests + testStore() helper
│       ├── project.go                  # EXISTS as stub — future phase
│       ├── relation.go                 # EXISTS as stub — future phase
│       ├── tag.go                      # EXISTS as stub — future phase
│       └── task.go                     # EXISTS as stub — future phase
```

### What Already Exists

- **`migrations/001_initial.up.sql`** — The complete SQL DDL that creates all eight tables (projects, tasks, annotations, relations, tags, tag_assignments, workflows, workflow_transitions) plus indexes and seed data. You will NOT modify this file.
- **`go.mod`** — The module file already declares dependencies on `github.com/mattn/go-sqlite3 v1.14.38` and `github.com/google/uuid v1.6.0`.
- **`internal/sqlite/store.go`** — A stub file containing only `package sqlite`. We will replace its contents entirely.
- **Other `internal/sqlite/*.go` stubs** — Empty stubs for future repo files. We will not touch them in this phase.

---

## Key Concepts

Before diving into the code, here is an explanation of every concept you will encounter. Read these so the implementation steps make sense.

- **`go:embed`** — A Go compiler directive (written as a comment like `//go:embed *.sql`) that tells the Go compiler to read files from disk at build time and bake their contents into the compiled binary. This means when you distribute the Tusk binary, it already contains the SQL migration files inside itself — no need to ship separate `.sql` files alongside it. The embedded files are accessed through an `embed.FS` value, which implements Go's `fs.FS` interface (a read-only filesystem).

- **`database/sql`** — Go's standard library package for talking to SQL databases. It provides a generic interface (`*sql.DB`) that works with any database driver. You call `sql.Open("sqlite3", path)` to get a `*sql.DB`, and then use methods like `.Exec()` (run a statement), `.Query()` (get rows), and `.QueryRow()` (get one row). The `*sql.DB` manages a pool of connections internally, so it is safe to use from multiple goroutines.

- **`CGO_ENABLED=1`** — The `go-sqlite3` driver is a Go wrapper around the C SQLite library. Because it calls C code, Go's "cgo" feature must be enabled at build time. On macOS this typically works out of the box because Xcode command-line tools provide a C compiler. If you see errors about "cgo" or "gcc", make sure `CGO_ENABLED=1` is set in your environment (it is the default on macOS).

- **SQLite WAL mode** — WAL stands for Write-Ahead Logging. By default, SQLite locks the entire database file during writes, blocking all readers. WAL mode changes this: readers can read while a writer is writing, because writes go to a separate "WAL file" first. This is critical for Tusk because the CLI and MCP server might read and write concurrently. We enable it with `PRAGMA journal_mode=WAL`.

- **SQLite PRAGMA** — PRAGMAs are special SQLite commands that configure the database engine's behavior. They look like `PRAGMA key=value`. Important: most PRAGMAs cannot run inside a transaction — they must be executed as standalone statements. That is why our code sets PRAGMAs before starting any migration transactions, and why the migration runner strips PRAGMA lines from the `.sql` files (to avoid running them inside the migration transaction).

- **Migration pattern** — A migration is a versioned SQL script that evolves the database schema over time. Each migration has a version number (like `001`) and an "up" script (to apply the change) and a "down" script (to undo it). A `schema_migrations` table tracks which versions have been applied. When the app starts, the migration runner compares the files on disk to the `schema_migrations` table and applies any new ones. This way, the database schema always matches what the code expects.

- **`sql.NullString`** — In SQL, a column can be `NULL` (no value). Go's basic `string` type cannot represent `NULL` — it always has a value (at minimum, the empty string `""`). The `sql.NullString` struct solves this: it has two fields, `String` (the value) and `Valid` (a boolean — `true` means there is a value, `false` means `NULL`). We use it when scanning nullable columns from the database.

- **Go test helpers** — `t.Helper()` tells Go's testing framework that the current function is a helper, so when a test fails, the error message shows the line number of the *calling* test, not the helper. `t.Cleanup(func())` registers a function to run when the test finishes (like a deferred cleanup). These make test output easier to read and prevent resource leaks. Functions defined in one `_test.go` file in a package are visible to all other `_test.go` files in the same package, so `testStore(t)` defined in `store_test.go` can be called from `task_test.go`.

- **`timeFormat` constant** — SQLite stores timestamps as text strings. Our schema uses `strftime('%Y-%m-%dT%H:%M:%fZ', 'now')` which produces strings like `2026-04-01T14:30:00.000Z`. The Go constant `timeFormat = "2006-01-02T15:04:05.000Z"` is Go's way of describing the same format (Go uses a reference date — January 2, 2006 at 3:04:05 PM — instead of `%Y-%m-%d` tokens). We need this constant everywhere we parse or format timestamps to ensure Go and SQLite agree on the format.

---

## Wiring: How This Phase Connects to Others

This phase produces four things that every subsequent phase depends on:

1. **`Store.DB()` returns `*sql.DB`** — Every repository struct in later phases (e.g., `TaskRepo`, `ProjectRepo`) will be constructed with `NewTaskRepo(db *sql.DB)`. They get that `*sql.DB` by calling `store.DB()`. Without the Store, no repo can talk to the database.

2. **`testStore(t)` helper function** — Every test file in later phases (e.g., `task_test.go`, `project_test.go`) will call `testStore(t)` to get a fresh in-memory SQLite database with all migrations applied. This gives each test a clean database in milliseconds (no disk I/O, no cleanup needed).

3. **Helper functions (`nullableUUID`, `nullableTime`, `nullableString`, `parseUUID`, `parseTime`, `marshalJSON`)** — These are used by every repository implementation to convert between Go types and SQLite column values. For example, a task's `parent_id` is `*uuid.UUID` in Go but `TEXT` (nullable) in SQLite. `nullableUUID` converts Go-to-SQL, and `parseUUID` converts SQL-to-Go.

4. **`timeFormat` constant and `mustTimeNow()` test helper** — Used everywhere for consistent time serialization and in tests for creating timestamps.

---

## Files You'll Create/Modify

| File | Action | Purpose |
|---|---|---|
| `migrations/migrations.go` | **Create** | Embeds `*.sql` files into the binary via `go:embed` |
| `internal/sqlite/store.go` | **Replace** (was empty stub) | `Store` struct: open DB, set pragmas, run migrations, helper functions |
| `internal/sqlite/store_test.go` | **Create** | Tests for Store + `testStore(t)` helper for all future tests |

---

## Task 1: Create the Migrations Embed Package

**Why:** The SQL migration files live on disk in the `migrations/` directory. When we build Tusk into a single binary, those `.sql` files would normally be left behind — the binary would not know how to find them. Go's `go:embed` directive solves this by reading the files at compile time and storing their contents inside the binary itself. We need a tiny Go package in the `migrations/` directory that declares an embedded filesystem variable. This variable will be passed to the Store so it can read and execute the SQL files.

**Files:**
- Create: `migrations/migrations.go`

- [ ] **Step 1: Create `migrations/migrations.go`**

This file does one thing: it tells the Go compiler to embed every `.sql` file in the `migrations/` directory into an `embed.FS` variable called `FS`. Other packages will import this package and use `migrations.FS` to access the embedded SQL files.

Create the file at `/Users/germanamz/projects/tusk/migrations/migrations.go` with this exact content:

```go
// Package migrations embeds the SQL migration files into the compiled binary.
//
// Usage from other packages:
//
//	import "github.com/germanamz/tusk/migrations"
//	store, err := sqlite.New(":memory:", migrations.FS)
//
// The go:embed directive below tells the Go compiler to read every .sql file
// in this directory at build time and store their contents in the FS variable.
// At runtime, FS behaves like a read-only filesystem containing those files.
package migrations

import "embed"

// FS contains all *.sql files from the migrations directory, embedded at compile time.
// It implements the fs.FS interface, which means you can use standard fs package
// functions like fs.Glob(), fs.ReadFile(), etc. to access the files.
//
//go:embed *.sql
var FS embed.FS
```

Here is what each part does:

- `package migrations` — Declares this file as part of the `migrations` package. Any Go file in the `migrations/` directory must have this package name.
- `import "embed"` — Imports Go's embed package. You must import this package for `//go:embed` to work, even though you don't call any functions from it directly. The compiler uses the import to know that embedding is in use.
- `//go:embed *.sql` — This is a **compiler directive**, not a regular comment. It must be on the line immediately before the variable declaration, with no blank line between them. The `*.sql` glob pattern means "embed every file ending in `.sql` in this directory." At compile time, the compiler reads `001_initial.up.sql`, `001_initial.down.sql`, and any future migration files, and stores their contents in the `FS` variable.
- `var FS embed.FS` — Declares a package-level variable of type `embed.FS`. This type implements Go's `fs.FS` interface, which is a standard read-only filesystem interface. Other code can call `fs.ReadFile(migrations.FS, "001_initial.up.sql")` to get the file contents as a byte slice.

- [ ] **Step 2: Verify the file compiles**

Run this command from the project root to make sure the package compiles without errors:

```bash
cd /Users/germanamz/projects/tusk && go build ./migrations/
```

**What to expect:** No output means success. If you see an error like `pattern *.sql: no matching files found`, it means the `.sql` files are not in the `migrations/` directory — double-check that `001_initial.up.sql` exists there.

- [ ] **Step 3: Commit**

```bash
cd /Users/germanamz/projects/tusk && git add migrations/migrations.go && git commit -m "feat(migrations): add go:embed package to bundle SQL files into binary"
```

---

## Task 2: Implement the Store

**Why:** The Store is the central piece of infrastructure for the entire SQLite layer. It is responsible for: (a) opening a connection to the SQLite database file, (b) configuring SQLite for concurrent access via PRAGMAs, (c) running database migrations so the schema is always up to date, and (d) providing shared helper functions that all repository implementations will use to convert between Go types and SQL values. Without the Store, nothing else in the `internal/sqlite/` package can function.

**Files:**
- Replace: `internal/sqlite/store.go` (currently an empty stub with just `package sqlite`)

- [ ] **Step 1: Replace `internal/sqlite/store.go` with the full implementation**

Replace the entire contents of `/Users/germanamz/projects/tusk/internal/sqlite/store.go` with this exact content:

```go
// Package sqlite implements the storage layer using SQLite.
// This file contains the Store struct which manages the database connection,
// runs migrations on startup, and provides shared helper functions used by
// all repository implementations.
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// timeFormat defines the timestamp format used throughout the application.
// It matches SQLite's strftime('%Y-%m-%dT%H:%M:%fZ', 'now') output.
//
// Go uses a "reference time" to define date formats. The reference time is:
//   Mon Jan 2 15:04:05 MST 2006
// So "2006-01-02T15:04:05.000Z" means:
//   2006 = four-digit year
//   01   = two-digit month
//   02   = two-digit day
//   15   = two-digit hour (24-hour clock)
//   04   = two-digit minute
//   05   = two-digit second
//   .000 = three-digit millisecond
//   Z    = literal "Z" meaning UTC timezone
const timeFormat = "2006-01-02T15:04:05.000Z"

// Store manages the SQLite database connection and provides shared
// infrastructure for all repository implementations.
//
// It is created via New(), which opens the database, configures SQLite
// pragmas for optimal concurrent performance, and runs any pending
// migrations. Repository structs (TaskRepo, ProjectRepo, etc.) receive
// the *sql.DB from Store.DB() to execute their queries.
type Store struct {
	db *sql.DB
}

// New creates a new Store by opening a SQLite database at dbPath, configuring
// it for concurrent access, and running all pending migrations from migrationsFS.
//
// Parameters:
//   - dbPath: Path to the SQLite database file (e.g., "~/.tusk/tusk.db").
//     Use ":memory:" for an in-memory database (useful for testing).
//   - migrationsFS: An fs.FS containing the .sql migration files.
//     Typically this is migrations.FS from the migrations package.
//
// The function performs these steps in order:
//  1. Opens the SQLite database connection
//  2. Pings the database to verify the connection works
//  3. Sets PRAGMAs (WAL mode, busy timeout, foreign keys)
//  4. Runs any pending migrations
//
// If any step fails, the database is closed and an error is returned.
func New(dbPath string, migrationsFS fs.FS) (*Store, error) {
	// sql.Open creates a *sql.DB, which is a connection pool manager.
	// The first argument "sqlite3" tells database/sql to use the go-sqlite3 driver.
	// The second argument is the "data source name" — for SQLite, this is the file path.
	// Note: sql.Open does NOT actually open a connection or touch the file yet.
	// It only validates the driver name and saves the DSN for later.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// db.Ping() forces the database to actually open a connection and verify
	// it works. This is where file-not-found or permission errors will surface.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// Set SQLite PRAGMAs. These configure the database engine's behavior.
	// IMPORTANT: PRAGMAs must be executed OUTSIDE of any transaction because
	// some of them (like journal_mode) change the database file format and
	// cannot take effect inside a transaction.
	//
	// - journal_mode=WAL: Enables Write-Ahead Logging. This allows multiple
	//   readers to read simultaneously while a writer is writing. Without this,
	//   SQLite would lock the entire database during writes.
	//
	// - busy_timeout=5000: If the database is locked by another connection,
	//   wait up to 5000 milliseconds (5 seconds) before returning a "database
	//   is locked" error. Without this, SQLite would fail immediately.
	//
	// - foreign_keys=ON: Enables foreign key constraint enforcement. SQLite
	//   has foreign key syntax but does NOT enforce it by default — you must
	//   explicitly turn it on. Without this, you could insert a task with a
	//   project_id that doesn't exist in the projects table.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting %s: %w", pragma, err)
		}
	}

	s := &Store{db: db}

	// Run migrations to ensure the database schema is up to date.
	// This reads .sql files from migrationsFS and applies any that
	// haven't been applied yet.
	if err := s.migrate(migrationsFS); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

// DB returns the underlying *sql.DB connection pool.
// Repository structs call this to get the database handle they need
// to execute queries. For example:
//
//	taskRepo := sqlite.NewTaskRepo(store.DB())
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database connection. Always call this when the
// application shuts down to release the database file lock and flush
// any pending WAL data. In tests, t.Cleanup() handles this automatically.
func (s *Store) Close() error { return s.db.Close() }

// migrate applies pending database migrations from the provided filesystem.
//
// How it works:
//  1. Creates a schema_migrations table if it doesn't exist. This table
//     tracks which migration versions have already been applied.
//  2. Reads all applied versions from schema_migrations into a map.
//  3. Finds all *.up.sql files in migrationsFS, sorted by name (which
//     sorts by version number since filenames start with "001_", "002_", etc.).
//  4. For each migration file that hasn't been applied yet:
//     a. Strips PRAGMA lines (they were already set outside any transaction)
//     b. Wraps the SQL in a transaction
//     c. Executes the SQL
//     d. Records the version in schema_migrations
//     e. Commits the transaction
//
// If any migration fails, its transaction is rolled back and the error
// is returned. Already-applied migrations are never re-applied.
func (s *Store) migrate(migrationsFS fs.FS) error {
	// Create the schema_migrations table if it doesn't exist.
	// This uses IF NOT EXISTS so it's safe to call multiple times.
	// The table has two columns:
	//   - version: The migration number (e.g., 1 for 001_initial.up.sql)
	//   - applied_at: When the migration was applied (for debugging/auditing)
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	// Read which versions have already been applied.
	// We store them in a map for O(1) lookup: applied[1] = true means
	// version 1 has been applied.
	applied := map[int]bool{}
	rows, err := s.db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("reading applied migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return err
		}
		applied[v] = true
	}
	// rows.Err() checks for errors that occurred during iteration.
	// Always check this after a rows.Next() loop — if the database
	// connection drops mid-iteration, rows.Next() returns false (ending
	// the loop) but the error is only available via rows.Err().
	if err := rows.Err(); err != nil {
		return err
	}

	// Find all *.up.sql files in the migrations filesystem.
	// fs.Glob works like filepath.Glob but on an fs.FS.
	// For our embedded filesystem, this will find "001_initial.up.sql"
	// and any future migration files like "002_add_columns.up.sql".
	entries, err := fs.Glob(migrationsFS, "*.up.sql")
	if err != nil {
		return fmt.Errorf("listing migration files: %w", err)
	}

	// Sort the filenames alphabetically. Since they start with zero-padded
	// numbers ("001_", "002_"), alphabetical order IS numerical order.
	sort.Strings(entries)

	for _, name := range entries {
		// Extract the version number from the filename.
		// fmt.Sscanf with "%d_" reads an integer followed by an underscore.
		// For "001_initial.up.sql", version will be 1.
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("parsing version from %s: %w", name, err)
		}

		// Skip this migration if it has already been applied.
		if applied[version] {
			continue
		}

		// Read the SQL file contents.
		data, err := fs.ReadFile(migrationsFS, name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}

		// Strip PRAGMA lines from the SQL. PRAGMAs were already executed
		// in New() outside any transaction. Running them again inside a
		// transaction would either fail or have no effect, so we remove them.
		statements := stripPragmas(string(data))

		// Begin a transaction. All the CREATE TABLE, INSERT, etc. statements
		// in the migration file will execute inside this transaction.
		// If anything fails, we roll back — the database stays unchanged.
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("beginning tx for %s: %w", name, err)
		}

		// Execute all the SQL statements from the migration file.
		// tx.Exec can handle multiple statements separated by semicolons.
		if _, err := tx.Exec(statements); err != nil {
			tx.Rollback()
			return fmt.Errorf("executing %s: %w", name, err)
		}

		// Record this migration version in schema_migrations so we don't
		// apply it again next time.
		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			version, time.Now().UTC().Format(timeFormat),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording %s: %w", name, err)
		}

		// Commit the transaction. If this succeeds, all the schema changes
		// and the schema_migrations record are permanently saved.
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing %s: %w", name, err)
		}
	}
	return nil
}

// stripPragmas removes PRAGMA lines from a SQL string.
//
// Why: PRAGMA statements cannot run inside a transaction (SQLite silently
// ignores them or errors). Since our migration runner wraps each migration
// file in a transaction, we need to strip PRAGMA lines. The PRAGMAs are
// already applied in New() before migrations run.
//
// How it works: splits the SQL into lines, checks if each line (after
// trimming whitespace and converting to uppercase) starts with "PRAGMA ",
// and skips those lines. All other lines (CREATE TABLE, INSERT, etc.)
// are kept.
func stripPragmas(sql string) string {
	var lines []string
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(strings.ToUpper(line))
		if strings.HasPrefix(trimmed, "PRAGMA ") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Shared helper functions for repository implementations
// ---------------------------------------------------------------------------
//
// The functions below convert between Go types and SQLite column values.
// SQLite stores everything as TEXT, INTEGER, REAL, or BLOB. Our Go domain
// types use uuid.UUID, time.Time, *string, etc. These helpers bridge the gap.
//
// "nullable" helpers convert Go → SQL (for INSERT/UPDATE parameters).
// "parse" helpers convert SQL → Go (for scanning query results).

// nullableUUID converts a *uuid.UUID to a value suitable for a SQL parameter.
// If the pointer is nil, it returns nil (which becomes SQL NULL).
// If the pointer is non-nil, it returns the UUID as a string (e.g., "550e8400-...").
//
// Used when inserting/updating columns like tasks.parent_id or tasks.project_id,
// which are optional foreign keys (TEXT columns that can be NULL).
func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

// nullableTime converts a *time.Time to a value suitable for a SQL parameter.
// If the pointer is nil, it returns nil (SQL NULL).
// If non-nil, it formats the time as a UTC string in our standard timeFormat.
//
// Used for optional timestamp columns like tasks.due_at and tasks.wait_until.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeFormat)
}

// nullableString converts a *string to a value suitable for a SQL parameter.
// If the pointer is nil, it returns nil (SQL NULL).
// If non-nil, it returns the string value.
//
// Used for optional text columns like tags.color.
func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// parseUUID converts a sql.NullString (scanned from a nullable TEXT column)
// back into a *uuid.UUID.
// If the column was NULL (ns.Valid == false), it returns nil.
// If the column had a value, it parses it as a UUID and returns a pointer to it.
//
// Used when scanning columns like tasks.parent_id from query results.
func parseUUID(ns sql.NullString) (*uuid.UUID, error) {
	if !ns.Valid {
		return nil, nil
	}
	id, err := uuid.Parse(ns.String)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// parseTime converts a sql.NullString (scanned from a nullable TEXT column)
// back into a *time.Time.
// If the column was NULL, it returns nil.
// If the column had a value, it parses it using our timeFormat.
//
// Used when scanning columns like tasks.due_at from query results.
func parseTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := time.Parse(timeFormat, ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// marshalJSON converts a Go value to a JSON string for storage in a TEXT column.
// If the value is nil, it returns "{}" (empty JSON object).
// Used for columns like tasks.uda which store arbitrary JSON data.
//
// The error from json.Marshal is intentionally ignored because we only pass
// known-safe types (maps, slices) that always marshal successfully.
func marshalJSON(v any) string {
	if v == nil {
		return "{}"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
```

Here is a walkthrough of the major sections:

**Imports explained:**
- `"database/sql"` — Go's standard database interface. Provides `*sql.DB`, `*sql.Rows`, `sql.NullString`, etc.
- `"encoding/json"` — For converting Go values to/from JSON strings (used in `marshalJSON`).
- `"fmt"` — String formatting and error wrapping with `fmt.Errorf("context: %w", err)`.
- `"io/fs"` — The `fs.FS` interface and helper functions (`fs.Glob`, `fs.ReadFile`). This is what `embed.FS` implements.
- `"sort"` — For sorting migration filenames alphabetically.
- `"strings"` — String manipulation (`Split`, `Join`, `TrimSpace`, `ToUpper`, `HasPrefix`).
- `"time"` — For formatting timestamps.
- `"github.com/google/uuid"` — UUID parsing and generation, used by the helper functions.
- `_ "github.com/mattn/go-sqlite3"` — The **blank import** (`_` means "import for side effects only"). The `go-sqlite3` package's `init()` function registers itself as the `"sqlite3"` driver with `database/sql`. We never call any of its functions directly — we just need it to be registered so `sql.Open("sqlite3", ...)` works.

**The `Store` struct** holds a single field: `db *sql.DB`. It is intentionally minimal. The Store is not a repository — it does not have methods like `CreateTask()`. It is infrastructure that repository structs build on.

**The `New` constructor** follows a defensive pattern: if any step fails after the database is opened, it calls `db.Close()` before returning the error. This prevents leaking database connections.

**The `migrate` method** is the most complex part. It implements a simple but effective migration system: read which versions are applied, find which files exist, apply any new ones in order.

**The helper functions** follow two naming conventions:
- `nullable*` — Convert a Go pointer type to `any` (Go's "any value" type, used for SQL parameters). Returns `nil` for null pointers (which SQLite interprets as NULL).
- `parse*` — Convert a `sql.NullString` (scanned from SQLite) back to a Go pointer type. Returns `nil` for NULL columns.

- [ ] **Step 2: Verify the file compiles**

Run this command to verify the Store compiles:

```bash
cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/
```

**What to expect:** No output means success. If you see import errors, make sure `go.mod` has the correct dependencies and run `go mod tidy`.

- [ ] **Step 3: Commit**

```bash
cd /Users/germanamz/projects/tusk && git add internal/sqlite/store.go && git commit -m "feat(sqlite): implement Store with connection management, migration runner, and helper functions"
```

---

## Task 3: Write the Store Tests

**Why:** Tests serve two purposes here. First, they verify that the Store actually works — that it can open a database, set PRAGMAs, run migrations, and that migrations are idempotent (running them twice does not break anything). Second, and equally important, this test file defines the `testStore(t)` helper function that ALL future test files in the `internal/sqlite/` package will use. Every repository test (e.g., `task_test.go`, `project_test.go`) will call `testStore(t)` to get a fresh, migrated, in-memory database.

We follow a test-after approach here (rather than strict TDD) because the Store is infrastructure — it needs to work end-to-end before we can meaningfully test it. The tests verify the complete behavior.

**Files:**
- Create: `internal/sqlite/store_test.go`

- [ ] **Step 1: Create `internal/sqlite/store_test.go`**

Create the file at `/Users/germanamz/projects/tusk/internal/sqlite/store_test.go` with this exact content:

```go
// Package sqlite tests.
//
// This file contains tests for the Store struct and also defines two helper
// functions — testStore(t) and mustTimeNow() — that are used by ALL test
// files in this package (including future files like task_test.go,
// project_test.go, etc.).
//
// IMPORTANT: Go test helpers defined in one _test.go file are visible to
// all other _test.go files in the same package. So testStore(t) defined
// here can be called from task_test.go without any import.
package sqlite

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/migrations"
)

// testStore creates a fresh in-memory SQLite database with all migrations
// applied. It is the foundation for every test in the sqlite package.
//
// How it works:
//   - Calls New(":memory:", migrations.FS) to create an in-memory database.
//     ":memory:" is a special SQLite path that creates a temporary database
//     in RAM — it's fast (no disk I/O) and disappears when the connection closes.
//   - migrations.FS is the embedded filesystem from the migrations package,
//     containing our .sql files.
//   - t.Helper() marks this function as a test helper. When a test that calls
//     testStore(t) fails, Go will report the failure at the line in the TEST
//     that called testStore, not at the line inside testStore. This makes
//     debugging much easier.
//   - t.Cleanup(func() { s.Close() }) registers a cleanup function that runs
//     when the test finishes (whether it passes or fails). This ensures the
//     database connection is always closed, preventing resource leaks.
//   - t.Fatalf() is like t.Errorf() but it immediately stops the test.
//     We use it here because if the store can't be created, there's no point
//     continuing — every subsequent operation would fail anyway.
func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// mustTimeNow returns the current UTC time truncated to millisecond precision.
//
// Why truncate? SQLite stores timestamps with millisecond precision (via
// strftime('%Y-%m-%dT%H:%M:%fZ')). Go's time.Now() has nanosecond precision.
// If we create a time in Go, store it in SQLite, and read it back, the
// nanosecond digits will be lost. Truncating to milliseconds before storing
// ensures the "before" and "after" values match when we compare them in tests.
//
// Why UTC? All timestamps in Tusk are stored in UTC to avoid timezone
// confusion. time.Now().UTC() converts the local time to UTC.
func mustTimeNow() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

// TestNew verifies that a Store can be created and that its DB() method
// returns a non-nil *sql.DB. This is the most basic smoke test — if this
// fails, nothing else in the sqlite package will work.
func TestNew(t *testing.T) {
	s := testStore(t)
	if s.DB() == nil {
		t.Fatal("expected non-nil *sql.DB")
	}
}

// TestPragmas verifies that the three SQLite PRAGMAs we set in New() are
// actually in effect. Each PRAGMA is checked by querying its current value.
//
// This test matters because:
//   - Without WAL mode, concurrent reads would be blocked during writes.
//   - Without foreign_keys=ON, SQLite would silently allow orphaned references.
//   - Without busy_timeout, concurrent access would fail immediately instead
//     of waiting.
func TestPragmas(t *testing.T) {
	s := testStore(t)

	// Check journal_mode is "wal" (SQLite returns it in lowercase).
	var journalMode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	// Note: For :memory: databases, journal_mode might be "memory" instead of "wal"
	// because in-memory databases don't have a file to write a WAL to.
	// However, go-sqlite3 with :memory: returns "wal" on this PRAGMA query
	// because the PRAGMA was successfully set (even though it has no practical
	// effect for in-memory databases).
	if journalMode != "wal" {
		t.Fatalf("expected wal, got %s", journalMode)
	}

	// Check foreign_keys is enabled (1 = ON, 0 = OFF).
	var fk int
	if err := s.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}

	// Check busy_timeout is 5000 milliseconds.
	var busyTimeout int
	if err := s.DB().QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", busyTimeout)
	}
}

// TestMigrations verifies that the migration runner correctly applied our
// 001_initial.up.sql migration. It checks three things:
//
//  1. The schema_migrations table has exactly 1 row (one migration applied).
//  2. The seed data was inserted (the _default project exists).
//  3. All expected tables were created.
//
// If this test fails, it means the migration runner has a bug — either it
// didn't find the .sql files, didn't execute them, or didn't record them.
func TestMigrations(t *testing.T) {
	s := testStore(t)

	// Verify that exactly 1 migration has been recorded in schema_migrations.
	var count int
	err := s.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration applied, got %d", count)
	}

	// Verify the seed data: the _default project should exist.
	// This proves that not only were the CREATE TABLE statements executed,
	// but also the INSERT statements that create the default project.
	var name string
	err = s.DB().QueryRow("SELECT name FROM projects WHERE id = '00000000-0000-0000-0000-000000000000'").Scan(&name)
	if err != nil {
		t.Fatal(err)
	}
	if name != "_default" {
		t.Fatalf("expected _default project, got %s", name)
	}

	// Verify all expected tables exist by querying sqlite_master.
	// sqlite_master is a built-in SQLite table that lists all tables,
	// indexes, views, and triggers in the database.
	tables := []string{"projects", "tasks", "annotations", "relations", "tags", "tag_assignments", "workflows", "workflow_transitions"}
	for _, table := range tables {
		var n string
		err := s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
		if err != nil {
			t.Fatalf("table %s not found: %v", table, err)
		}
	}
}

// TestMigrationsIdempotent verifies that running migrations a second time
// does not cause errors or duplicate records.
//
// Why this matters: The Store runs migrations every time it starts up (every
// time New() is called). If a migration that was already applied tries to
// run again, it would fail (e.g., "table already exists"). Our migration
// runner prevents this by checking the schema_migrations table and skipping
// already-applied versions. This test proves that mechanism works.
func TestMigrationsIdempotent(t *testing.T) {
	s := testStore(t)

	// Call migrate() again — this simulates what happens when the application
	// is restarted and New() calls migrate() on an already-migrated database.
	err := s.migrate(migrations.FS)
	if err != nil {
		t.Fatalf("second migrate call failed: %v", err)
	}

	// Verify there's still exactly 1 migration record (not 2).
	var count int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration after idempotent call, got %d", count)
	}
}
```

Here is what each test does:

- **`TestNew`** — The most basic test. Creates a Store and checks that `DB()` returns something. If this fails, the Store constructor is broken.
- **`TestPragmas`** — Queries each PRAGMA to verify it was set correctly. This catches issues where the PRAGMA execution silently failed.
- **`TestMigrations`** — Checks that tables were created, seed data was inserted, and the migration was recorded. This validates the entire migration pipeline end-to-end.
- **`TestMigrationsIdempotent`** — Calls `migrate()` a second time on an already-migrated database. This validates the "skip already-applied" logic. Without this, restarting the application would crash.

- [ ] **Step 2: Run the tests**

Run all four tests with verbose output:

```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./internal/sqlite/ -v -count=1
```

The `-v` flag shows each test name and its PASS/FAIL status. The `-count=1` flag disables test caching so the tests always run fresh.

**What to expect:**

```
=== RUN   TestNew
--- PASS: TestNew (0.XXs)
=== RUN   TestPragmas
--- PASS: TestPragmas (0.XXs)
=== RUN   TestMigrations
--- PASS: TestMigrations (0.XXs)
=== RUN   TestMigrationsIdempotent
--- PASS: TestMigrationsIdempotent (0.XXs)
PASS
ok      github.com/germanamz/tusk/internal/sqlite       0.XXXs
```

All four tests should show `PASS`. The exact times (`0.XXs`) will vary but should be under 1 second since we use in-memory databases.

If you see `FAIL` for any test, read the error message carefully. Common issues:
- `"cgo: ..."` errors — Make sure `CGO_ENABLED=1` is set and you have a C compiler (Xcode CLI tools on macOS).
- `"opening test store: ..."` — The Store constructor failed. Check that `migrations/migrations.go` exists and the `.sql` file is valid.
- `"table X not found"` — The migration SQL has a syntax error or didn't execute fully.

- [ ] **Step 3: Commit**

```bash
cd /Users/germanamz/projects/tusk && git add internal/sqlite/store_test.go && git commit -m "test(sqlite): add Store tests and testStore(t) helper for future repository tests"
```

---

## Final Verification

After completing all three tasks, run the full test suite one more time to make sure everything works together:

```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./... -count=1
```

This runs ALL tests in ALL packages in the project. You should see output similar to:

```
ok      github.com/germanamz/tusk/internal/domain       0.XXXs
ok      github.com/germanamz/tusk/internal/repository    0.XXXs
ok      github.com/germanamz/tusk/internal/sqlite        0.XXXs
```

Every package should show `ok`. If any package shows `FAIL`, investigate that package specifically with `go test ./path/to/package -v`.

Also verify that the three new/modified files are committed:

```bash
cd /Users/germanamz/projects/tusk && git log --oneline -3
```

You should see three commits (most recent first):

```
aaaaaaa test(sqlite): add Store tests and testStore(t) helper for future repository tests
bbbbbbb feat(sqlite): implement Store with connection management, migration runner, and helper functions
ccccccc feat(migrations): add go:embed package to bundle SQL files into binary
```

(The commit hashes will be different.)

---

## Summary of What Was Built

| File | What it does |
|---|---|
| `migrations/migrations.go` | Embeds all `*.sql` files into the binary so they're available at runtime without shipping separate files |
| `internal/sqlite/store.go` | Opens SQLite, configures WAL/FK/timeout, runs migrations, provides type-conversion helpers |
| `internal/sqlite/store_test.go` | Validates the Store works end-to-end, defines `testStore(t)` that all future tests will use |

**Next phase** will use this foundation to implement the first repository: `TaskRepo` in `internal/sqlite/task.go`, using `Store.DB()` for queries and `testStore(t)` for tests.
