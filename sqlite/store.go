// Package sqlite implements the storage layer using SQLite.
// This file contains the Store struct which manages the database connection,
// runs migrations on startup, and provides shared helper functions used by
// all repository implementations.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// DBTX is the common interface between *sql.DB and *sql.Tx.
// All repository implementations use only these three methods, so they can
// operate on either a raw connection pool or an active transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// timeFormat defines the timestamp format used throughout the application.
// It matches SQLite's strftime('%Y-%m-%dT%H:%M:%fZ', 'now') output.
const timeFormat = "2006-01-02T15:04:05.000Z"

// Store manages the SQLite database connection and provides shared
// infrastructure for all repository implementations.
type Store struct {
	db *sql.DB
}

// New creates a new Store by opening a SQLite database at dbPath, configuring
// it for concurrent access, and running all pending migrations from migrationsFS.
func New(dbPath string, migrationsFS fs.FS) (*Store, error) {
	// Pragmas are set via DSN parameters so they apply to every connection
	// opened by the pool, not just the first one. foreign_keys is per-connection
	// and would silently default to OFF on new pooled connections without this.
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)

	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	store := &Store{db: db}

	if err := store.migrate(migrationsFS); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// DB returns the underlying *sql.DB connection pool.
func (store *Store) DB() *sql.DB { return store.db }

// Close closes the database connection.
func (store *Store) Close() error { return store.db.Close() }

// Tx wraps an active database transaction and provides access to
// transactional repository instances. Repos created from a Tx share
// the same underlying *sql.Tx, so all their operations are atomic.
type Tx struct {
	tx *sql.Tx
}

// Tasks returns a TaskRepo operating within this transaction.
func (txw *Tx) Tasks() *TaskRepo { return NewTaskRepo(txw.tx) }

// Relations returns a RelationRepo operating within this transaction.
func (txw *Tx) Relations() *RelationRepo { return NewRelationRepo(txw.tx) }

// Annotations returns an AnnotationRepo operating within this transaction.
func (txw *Tx) Annotations() *AnnotationRepo { return NewAnnotationRepo(txw.tx) }

// Notes returns a NoteRepo operating within this transaction.
func (txw *Tx) Notes() *NoteRepo { return NewNoteRepo(txw.tx) }

// Tags returns a TagRepo operating within this transaction.
func (txw *Tx) Tags() *TagRepo { return NewTagRepo(txw.tx) }

// Projects returns a ProjectRepo operating within this transaction.
func (txw *Tx) Projects() *ProjectRepo { return NewProjectRepo(txw.tx) }

// Workflows returns a WorkflowRepo operating within this transaction.
func (txw *Tx) Workflows() *WorkflowRepo { return NewWorkflowRepo(txw.tx) }

// Players returns a PlayerRepo operating within this transaction.
func (txw *Tx) Players() *PlayerRepo { return NewPlayerRepo(txw.tx) }

// Events returns an EventRepo operating within this transaction. The retention
// parameters (maxEvents, pruneSlack) are attached at tx time because they are
// transaction-scoped policy, not repository-scoped.
func (txw *Tx) Events(maxEvents, pruneSlack int) *EventRepo {
	return NewEventRepo(txw.tx, maxEvents, pruneSlack)
}

// TruncateAll wipes every entity table inside this transaction in
// reverse-FK order. Used exclusively by the PortabilityService under
// --replace --truncate. Each DELETE is issued as a raw `DELETE FROM
// <table>` against txw.tx — no per-row WHERE clauses, no version checks.
// The single transaction wrapping the call rolls everything back
// atomically on any error.
func (txw *Tx) TruncateAll(ctx context.Context) error {
	tables := []string{
		"events", "notes", "annotations", "relations", "tag_assignments",
		"tasks", "projects", "workflows", "tags", "players",
	}
	for _, table := range tables {
		if _, err := txw.tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("truncating %s: %w", table, err)
		}
	}
	return nil
}

// WithTx executes fn within a database transaction. If fn returns nil,
// the transaction is committed. If fn returns an error (or panics),
// the transaction is rolled back and the error is returned.
func (store *Store) WithTx(ctx context.Context, fn func(tx *Tx) error) error {
	sqlTx, beginErr := store.db.BeginTx(ctx, nil)

	if beginErr != nil {
		return fmt.Errorf("beginning transaction: %w", beginErr)
	}

	defer sqlTx.Rollback() //nolint:errcheck // Rollback after commit returns sql.ErrTxDone, which is expected.

	if fnErr := fn(&Tx{tx: sqlTx}); fnErr != nil {
		return fnErr
	}

	return sqlTx.Commit()
}

// WithTaskTx executes fn with a TaskRepository backed by a transaction.
// This is the concrete implementation of service.TaskTxProvider.
func (store *Store) WithTaskTx(ctx context.Context, fn func(tasks repository.TaskRepository) error) error {
	return store.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks())
	})
}

// WithRelationTx executes fn with a RelationRepository backed by a transaction.
// This is the concrete implementation of service.RelationTxProvider.
func (store *Store) WithRelationTx(ctx context.Context, fn func(rr repository.RelationRepository) error) error {
	return store.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Relations())
	})
}

// WithProjectTx executes fn with a ProjectRepository and TaskRepository backed
// by the same transaction. This is the concrete implementation of
// service.ProjectTxProvider. Used by ProjectService.Delete to reassign
// referencing tasks off a project under --force before deleting the project
// row, so the FK on projects(id) does not fire.
func (store *Store) WithProjectTx(
	ctx context.Context,
	fn func(projects repository.ProjectRepository, tasks repository.TaskRepository) error,
) error {
	return store.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Projects(), tx.Tasks())
	})
}

// migrate applies pending database migrations from the provided filesystem.
func (store *Store) migrate(migrationsFS fs.FS) error {
	_, createErr := store.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)

	if createErr != nil {
		return fmt.Errorf("creating schema_migrations: %w", createErr)
	}

	applied := map[int]bool{}
	rows, queryErr := store.db.Query("SELECT version FROM schema_migrations")

	if queryErr != nil {
		return fmt.Errorf("reading applied migrations: %w", queryErr)
	}

	defer rows.Close()
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return err
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	entries, globErr := fs.Glob(migrationsFS, "*.up.sql")

	if globErr != nil {
		return fmt.Errorf("listing migration files: %w", globErr)
	}

	sort.Strings(entries)

	for _, name := range entries {
		var versionNum int
		if _, scanErr := fmt.Sscanf(name, "%d_", &versionNum); scanErr != nil {
			return fmt.Errorf("parsing version from %s: %w", name, scanErr)
		}

		if applied[versionNum] {
			continue
		}

		data, readErr := fs.ReadFile(migrationsFS, name)

		if readErr != nil {
			return fmt.Errorf("reading %s: %w", name, readErr)
		}

		statements := stripPragmas(string(data))

		migTx, beginErr := store.db.Begin()

		if beginErr != nil {
			return fmt.Errorf("beginning tx for %s: %w", name, beginErr)
		}

		if _, execErr := migTx.Exec(statements); execErr != nil {
			_ = migTx.Rollback()
			return fmt.Errorf("executing %s: %w", name, execErr)
		}

		if _, recordErr := migTx.Exec(
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			versionNum, time.Now().UTC().Format(timeFormat),
		); recordErr != nil {
			_ = migTx.Rollback()
			return fmt.Errorf("recording %s: %w", name, recordErr)
		}

		if commitErr := migTx.Commit(); commitErr != nil {
			return fmt.Errorf("committing %s: %w", name, commitErr)
		}
	}
	return nil
}

// stripPragmas removes PRAGMA lines from a SQL string.
// PRAGMAs cannot run inside a transaction and are already applied in New().
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

// nullableUUID converts a *uuid.UUID to a value suitable for a SQL parameter.
// If the pointer is nil, it returns nil (SQL NULL).
func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

// nullableTime converts a *time.Time to a value suitable for a SQL parameter.
// If the pointer is nil, it returns nil (SQL NULL).
func nullableTime(timePtr *time.Time) any {
	if timePtr == nil {
		return nil
	}
	return timePtr.UTC().Format(timeFormat)
}

// nullableString converts a *string to a value suitable for a SQL parameter.
// If the pointer is nil, it returns nil (SQL NULL).
func nullableString(str *string) any {
	if str == nil {
		return nil
	}
	return *str
}

// nullableFloat converts a *float64 to a value suitable for a SQL parameter.
// If the pointer is nil, it returns nil (SQL NULL).
func nullableFloat(floatPtr *float64) any {
	if floatPtr == nil {
		return nil
	}
	return *floatPtr
}

// nullableUrgencyOverrides converts a *domain.UrgencyOverrides to a value
// suitable for a SQL parameter. Returns nil (SQL NULL) when the pointer is
// nil; otherwise marshals the struct to JSON and returns the resulting
// string. Errors here surface JSON marshalling failures so the caller can
// wrap them with context.
func nullableUrgencyOverrides(overrides *domain.UrgencyOverrides) (any, error) {
	if overrides == nil {
		return nil, nil
	}
	jsonBytes, marshalErr := json.Marshal(overrides)

	if marshalErr != nil {
		return nil, fmt.Errorf("marshaling urgency_overrides: %w", marshalErr)
	}

	return string(jsonBytes), nil
}

// parseUUID converts a sql.NullString back into a *uuid.UUID.
// If the column was NULL, it returns nil.
func parseUUID(ns sql.NullString) (*uuid.UUID, error) {
	if !ns.Valid {
		return nil, nil
	}
	id, parseErr := uuid.Parse(ns.String)

	if parseErr != nil {
		return nil, parseErr
	}

	return &id, nil
}

// parseTime converts a sql.NullString back into a *time.Time.
// If the column was NULL, it returns nil.
func parseTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	parsedTime, parseErr := time.Parse(timeFormat, ns.String)

	if parseErr != nil {
		return nil, parseErr
	}

	return &parsedTime, nil
}

// marshalJSON converts a Go value to a JSON string for storage in a TEXT column.
// If the value is nil, it returns "{}" (empty JSON object).
func marshalJSON(val any) (string, error) {
	if val == nil {
		return "{}", nil
	}
	jsonBytes, marshalErr := json.Marshal(val)

	if marshalErr != nil {
		return "", fmt.Errorf("marshaling JSON: %w", marshalErr)
	}

	return string(jsonBytes), nil
}
