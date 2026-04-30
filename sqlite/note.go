package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// NoteRepo implements repository.NoteRepository using SQLite.
type NoteRepo struct {
	db DBTX
}

// NewNoteRepo creates a NoteRepo.
func NewNoteRepo(db DBTX) *NoteRepo {
	return &NoteRepo{db: db}
}

// Create inserts a new note. The caller must set ID, ProjectID, PlayerID,
// Body, and CreatedAt. TaskID and Metadata are optional.
func (repo *NoteRepo) Create(ctx context.Context, note *domain.Note) error {
	meta, err := marshalJSON(note.Metadata)

	if err != nil {
		return err
	}

	_, err = repo.db.ExecContext(ctx,
		`INSERT INTO notes (id, project_id, player_id, task_id, body, metadata, archived_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		note.ID.String(),
		note.ProjectID.String(),
		note.PlayerID,
		nullableUUID(note.TaskID),
		note.Body,
		meta,
		nullableTime(note.ArchivedAt),
		note.CreatedAt.UTC().Format(timeFormat),
	)
	return err
}

// GetByID retrieves a single note. Returns domain.ErrNotFound if missing.
func (repo *NoteRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Note, error) {
	row := repo.db.QueryRowContext(ctx,
		`SELECT id, project_id, player_id, task_id, body, metadata, archived_at, created_at
		 FROM notes WHERE id = ?`,
		id.String(),
	)
	return scanNote(row)
}

// FindByIDPrefix returns notes whose id (lowercase, hyphenated UUID string)
// begins with prefix. Returns up to 2 rows — callers only need to distinguish
// "zero matches", "one match", and "more than one match".
func (repo *NoteRepo) FindByIDPrefix(ctx context.Context, prefix string) ([]*domain.Note, error) {
	rows, err := repo.db.QueryContext(ctx, `
		SELECT id, project_id, player_id, task_id, body, metadata, archived_at, created_at
		FROM notes
		WHERE id GLOB ? || '*'
		ORDER BY id ASC
		LIMIT 2
	`, prefix)

	if err != nil {
		return nil, fmt.Errorf("querying notes by id prefix: %w", err)
	}

	defer rows.Close()

	var out []*domain.Note
	for rows.Next() {
		note, scanErr := scanNote(rows)

		if scanErr != nil {
			return nil, scanErr
		}

		out = append(out, note)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating notes by id prefix: %w", err)
	}
	return out, nil
}

// Archive sets the archived_at timestamp on a note.
// Returns domain.ErrNotFound if no note with that ID exists.
func (repo *NoteRepo) Archive(ctx context.Context, id uuid.UUID, archivedAt time.Time) error {
	res, execErr := repo.db.ExecContext(ctx,
		`UPDATE notes SET archived_at = ? WHERE id = ?`,
		archivedAt.UTC().Format(timeFormat),
		id.String(),
	)

	if execErr != nil {
		return execErr
	}

	rowCount, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowCount == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// List retrieves notes matching the given options, ordered by created_at DESC
// (newest first). Applies SQL-level filtering and LIMIT for the trailing window.
func (repo *NoteRepo) List(ctx context.Context, opts repository.NoteListOptions) ([]*domain.Note, error) {
	var (
		where []string
		args  []any
	)

	if opts.ProjectID != uuid.Nil {
		where = append(where, "project_id = ?")
		args = append(args, opts.ProjectID.String())
	}

	if opts.PlayerID != "" {
		where = append(where, "player_id = ?")
		args = append(args, opts.PlayerID)
	}

	if opts.TaskID != nil {
		where = append(where, "task_id = ?")
		args = append(args, opts.TaskID.String())
	}

	if opts.Since != nil {
		where = append(where, "created_at >= ?")
		args = append(args, opts.Since.UTC().Format(timeFormat))
	}

	if !opts.IncludeArchived {
		where = append(where, "archived_at IS NULL")
	}

	query := `SELECT id, project_id, player_id, task_id, body, metadata, archived_at, created_at FROM notes`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := repo.db.QueryContext(ctx, query, args...)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	result := make([]*domain.Note, 0)
	for rows.Next() {
		note, scanErr := scanNote(rows)

		if scanErr != nil {
			return nil, scanErr
		}

		result = append(result, note)
	}
	return result, rows.Err()
}

// noteScanner abstracts *sql.Row and *sql.Rows for scanNote.
type noteScanner interface {
	Scan(dest ...any) error
}

func scanNote(scanner noteScanner) (*domain.Note, error) {
	var (
		note               domain.Note
		id, projectID      string
		taskID, archivedAt sql.NullString
		metaStr, createdAt string
	)
	err := scanner.Scan(&id, &projectID, &note.PlayerID, &taskID, &note.Body, &metaStr, &archivedAt, &createdAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	note.ID, err = uuid.Parse(id)

	if err != nil {
		return nil, err
	}

	note.ProjectID, err = uuid.Parse(projectID)

	if err != nil {
		return nil, err
	}

	note.TaskID, err = parseUUID(taskID)

	if err != nil {
		return nil, err
	}

	note.ArchivedAt, err = parseTime(archivedAt)

	if err != nil {
		return nil, err
	}

	note.CreatedAt, err = time.Parse(timeFormat, createdAt)

	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(metaStr), &note.Metadata); err != nil {
		return nil, err
	}
	return &note, nil
}
