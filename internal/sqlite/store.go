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

	"github.com/germanamz/tusk/internal/repository"
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

	s := &Store{db: db}

	if err := s.migrate(migrationsFS); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

// DB returns the underlying *sql.DB connection pool.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Tx wraps an active database transaction and provides access to
// transactional repository instances. Repos created from a Tx share
// the same underlying *sql.Tx, so all their operations are atomic.
type Tx struct {
	tx *sql.Tx
}

// Tasks returns a TaskRepo operating within this transaction.
func (t *Tx) Tasks() *TaskRepo { return NewTaskRepo(t.tx) }

// Relations returns a RelationRepo operating within this transaction.
func (t *Tx) Relations() *RelationRepo { return NewRelationRepo(t.tx) }

// Annotations returns an AnnotationRepo operating within this transaction.
func (t *Tx) Annotations() *AnnotationRepo { return NewAnnotationRepo(t.tx) }

// Tags returns a TagRepo operating within this transaction.
func (t *Tx) Tags() *TagRepo { return NewTagRepo(t.tx) }

// Workflows returns a WorkflowRepo operating within this transaction.
func (t *Tx) Workflows() *WorkflowRepo { return NewWorkflowRepo(t.tx) }

// WithTx executes fn within a database transaction. If fn returns nil,
// the transaction is committed. If fn returns an error (or panics),
// the transaction is rolled back and the error is returned.
func (s *Store) WithTx(ctx context.Context, fn func(tx *Tx) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer sqlTx.Rollback() //nolint:errcheck // Rollback after commit returns sql.ErrTxDone, which is expected.

	if err := fn(&Tx{tx: sqlTx}); err != nil {
		return err
	}
	return sqlTx.Commit()
}

// WithTaskTx executes fn with TaskRepository and WorkflowRepository backed by
// a transaction. This is the concrete implementation of service.TaskTxProvider.
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, wr repository.WorkflowRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks(), tx.Workflows())
	})
}

// WithRelationTx executes fn with a RelationRepository backed by a transaction.
// This is the concrete implementation of service.RelationTxProvider.
func (s *Store) WithRelationTx(ctx context.Context, fn func(rr repository.RelationRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Relations())
	})
}

// migrate applies pending database migrations from the provided filesystem.
func (s *Store) migrate(migrationsFS fs.FS) error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

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
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := fs.Glob(migrationsFS, "*.up.sql")
	if err != nil {
		return fmt.Errorf("listing migration files: %w", err)
	}

	sort.Strings(entries)

	for _, name := range entries {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("parsing version from %s: %w", name, err)
		}

		if applied[version] {
			continue
		}

		data, err := fs.ReadFile(migrationsFS, name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}

		statements := stripPragmas(string(data))

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("beginning tx for %s: %w", name, err)
		}

		if _, err := tx.Exec(statements); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("executing %s: %w", name, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			version, time.Now().UTC().Format(timeFormat),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recording %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing %s: %w", name, err)
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
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeFormat)
}

// nullableString converts a *string to a value suitable for a SQL parameter.
// If the pointer is nil, it returns nil (SQL NULL).
func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// parseUUID converts a sql.NullString back into a *uuid.UUID.
// If the column was NULL, it returns nil.
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

// parseTime converts a sql.NullString back into a *time.Time.
// If the column was NULL, it returns nil.
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
func marshalJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshaling JSON: %w", err)
	}
	return string(b), nil
}
