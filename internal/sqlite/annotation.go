package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// AnnotationRepo implements repository.AnnotationRepository using SQLite.
//
// It stores *sql.DB (not *Store) so it depends only on the standard library's
// database abstraction. The Store is responsible for opening the DB and running
// migrations; AnnotationRepo just runs queries.
type AnnotationRepo struct {
	db *sql.DB
}

// NewAnnotationRepo creates an AnnotationRepo. Pass in the *sql.DB from Store.DB().
//
// Example:
//
//	store, _ := sqlite.New("tusk.db", migrations.FS)
//	repo := sqlite.NewAnnotationRepo(store.DB())
func NewAnnotationRepo(db *sql.DB) *AnnotationRepo {
	return &AnnotationRepo{db: db}
}

// Create inserts a new annotation into the database.
//
// The caller must set all fields on the Annotation struct before calling Create.
// The ID should be generated with uuid.New(), TaskID must reference an existing
// task, and CreatedAt should be set to time.Now().UTC().
//
// The annotation's task_id must reference an existing task (foreign key constraint).
// If the task does not exist, SQLite returns a foreign key violation error.
func (r *AnnotationRepo) Create(ctx context.Context, ann *domain.Annotation) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO annotations (id, task_id, body, created_at) VALUES (?, ?, ?, ?)`,
		ann.ID.String(), ann.TaskID.String(), ann.Body,
		ann.CreatedAt.UTC().Format(timeFormat),
	)
	return err
}

// GetByTask retrieves all annotations for a given task, ordered by creation time.
//
// Returns an empty slice (not nil and not an error) if the task has no annotations.
// The ORDER BY created_at ensures annotations appear in chronological order,
// which is the natural reading order for notes/comments.
func (r *AnnotationRepo) GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, task_id, body, created_at FROM annotations WHERE task_id = ? ORDER BY created_at`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Annotation, 0)
	for rows.Next() {
		var (
			a                  domain.Annotation
			id, tid, createdAt string
		)
		if err := rows.Scan(&id, &tid, &a.Body, &createdAt); err != nil {
			return nil, err
		}
		a.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		a.TaskID, err = uuid.Parse(tid)
		if err != nil {
			return nil, err
		}
		a.CreatedAt, err = time.Parse(timeFormat, createdAt)
		if err != nil {
			return nil, err
		}
		result = append(result, &a)
	}
	return result, rows.Err()
}

// Delete removes an annotation by its ID.
// Returns domain.ErrNotFound if no annotation with that ID exists.
//
// This uses the RowsAffected pattern:
//  1. Run the DELETE statement. Even if no rows match, the SQL itself succeeds.
//  2. Check RowsAffected(). If it is 0, no row was deleted, meaning the ID
//     did not exist. We return domain.ErrNotFound in that case.
func (r *AnnotationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM annotations WHERE id = ?`, id.String())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
