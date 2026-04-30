package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// AnnotationRepo implements repository.AnnotationRepository using SQLite.
//
// It stores *sql.DB (not *Store) so it depends only on the standard library's
// database abstraction. The Store is responsible for opening the DB and running
// migrations; AnnotationRepo just runs queries.
type AnnotationRepo struct {
	db DBTX
}

// NewAnnotationRepo creates an AnnotationRepo. Pass in the *sql.DB from Store.DB().
//
// Example:
//
//	store, _ := sqlite.New("tusk.db", migrations.FS)
//	repo := sqlite.NewAnnotationRepo(store.DB())
func NewAnnotationRepo(db DBTX) *AnnotationRepo {
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
func (repo *AnnotationRepo) Create(ctx context.Context, annotation *domain.Annotation) error {
	_, err := repo.db.ExecContext(ctx,
		`INSERT INTO annotations (id, task_id, body, created_at) VALUES (?, ?, ?, ?)`,
		annotation.ID.String(), annotation.TaskID.String(), annotation.Body,
		annotation.CreatedAt.UTC().Format(timeFormat),
	)

	return err
}

// GetByTask retrieves all annotations for a given task, ordered by creation time.
//
// Returns an empty slice (not nil and not an error) if the task has no annotations.
// The ORDER BY created_at ensures annotations appear in chronological order,
// which is the natural reading order for notes/comments.
func (repo *AnnotationRepo) GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error) {
	rows, err := repo.db.QueryContext(ctx,
		`SELECT id, task_id, body, created_at FROM annotations WHERE task_id = ? ORDER BY created_at`,
		taskID.String())

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	result := make([]*domain.Annotation, 0)

	for rows.Next() {
		var (
			annotation       domain.Annotation
			idStr, taskIDStr string
			createdAt        string
		)

		if err := rows.Scan(&idStr, &taskIDStr, &annotation.Body, &createdAt); err != nil {
			return nil, err
		}

		annotation.ID, err = uuid.Parse(idStr)

		if err != nil {
			return nil, err
		}

		annotation.TaskID, err = uuid.Parse(taskIDStr)

		if err != nil {
			return nil, err
		}

		annotation.CreatedAt, err = time.Parse(timeFormat, createdAt)

		if err != nil {
			return nil, err
		}

		result = append(result, &annotation)
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
func (repo *AnnotationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, execErr := repo.db.ExecContext(ctx,
		`DELETE FROM annotations WHERE id = ?`, id.String())

	if execErr != nil {
		return execErr
	}

	rowsAffected, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowsAffected == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// annotationBatchSize is the maximum number of placeholders per SQL IN clause,
// chosen to stay well under SQLite's default SQLITE_MAX_VARIABLE_NUMBER (999).
const annotationBatchSize = 500

// GetByTasks returns annotations for multiple tasks in a single query, keyed
// by task ID. Each task's annotations are ordered ascending by created_at
// (chronological reading order). Tasks with no annotations are absent from
// the map. The returned map is always non-nil, even for empty input.
func (repo *AnnotationRepo) GetByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Annotation, error) {
	result := make(map[uuid.UUID][]*domain.Annotation, len(taskIDs))

	if len(taskIDs) == 0 {
		return result, nil
	}

	for start := 0; start < len(taskIDs); start += annotationBatchSize {
		end := start + annotationBatchSize
		if end > len(taskIDs) {
			end = len(taskIDs)
		}

		if err := repo.fetchAnnotationBatch(ctx, taskIDs[start:end], result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (repo *AnnotationRepo) fetchAnnotationBatch(ctx context.Context, taskIDs []uuid.UUID, dest map[uuid.UUID][]*domain.Annotation) error {
	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))

	for index, id := range taskIDs {
		placeholders[index] = "?"
		args[index] = id.String()
	}

	query := fmt.Sprintf(
		`SELECT id, task_id, body, created_at FROM annotations WHERE task_id IN (%s) ORDER BY task_id, created_at`,
		strings.Join(placeholders, ","),
	)

	rows, err := repo.db.QueryContext(ctx, query, args...)

	if err != nil {
		return err
	}

	defer rows.Close()

	for rows.Next() {
		var (
			annotation       domain.Annotation
			idStr, taskIDStr string
			createdAt        string
		)

		if err := rows.Scan(&idStr, &taskIDStr, &annotation.Body, &createdAt); err != nil {
			return err
		}

		annotation.ID, err = uuid.Parse(idStr)

		if err != nil {
			return err
		}

		annotation.TaskID, err = uuid.Parse(taskIDStr)

		if err != nil {
			return err
		}

		annotation.CreatedAt, err = time.Parse(timeFormat, createdAt)

		if err != nil {
			return err
		}

		dest[annotation.TaskID] = append(dest[annotation.TaskID], &annotation)
	}

	return rows.Err()
}

// CountByTasks returns annotation counts for each task ID in a single query.
// Tasks with zero annotations are not included in the returned map.
func (repo *AnnotationRepo) CountByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	if len(taskIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}

	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))

	for index, id := range taskIDs {
		placeholders[index] = "?"
		args[index] = id.String()
	}

	query := fmt.Sprintf(
		`SELECT task_id, COUNT(*) FROM annotations WHERE task_id IN (%s) GROUP BY task_id`,
		strings.Join(placeholders, ","),
	)

	rows, err := repo.db.QueryContext(ctx, query, args...)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	counts := make(map[uuid.UUID]int)

	for rows.Next() {
		var (
			idStr string
			count int
		)

		if err := rows.Scan(&idStr, &count); err != nil {
			return nil, err
		}

		taskID, parseErr := uuid.Parse(idStr)

		if parseErr != nil {
			return nil, fmt.Errorf("parsing task_id %q: %w", idStr, parseErr)
		}

		counts[taskID] = count
	}

	return counts, rows.Err()
}
