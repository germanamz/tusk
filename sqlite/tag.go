package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

type TagRepo struct {
	db DBTX
}

func NewTagRepo(db DBTX) *TagRepo {
	return &TagRepo{db: db}
}

func (repo *TagRepo) Create(ctx context.Context, tag *domain.Tag) error {
	_, err := repo.db.ExecContext(ctx,
		`INSERT INTO tags (id, name, color) VALUES (?, ?, ?)`,
		tag.ID.String(), tag.Name, nullableString(tag.Color),
	)
	return err
}

func (repo *TagRepo) GetByName(ctx context.Context, name string) (*domain.Tag, error) {
	row := repo.db.QueryRowContext(ctx,
		`SELECT id, name, color FROM tags WHERE name = ?`, name)
	tag, err := scanTag(row)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}

	return tag, err
}

func (repo *TagRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error) {
	row := repo.db.QueryRowContext(ctx,
		`SELECT id, name, color FROM tags WHERE id = ?`, id.String())
	tag, err := scanTag(row)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}

	return tag, err
}

func (repo *TagRepo) Update(ctx context.Context, tag *domain.Tag) error {
	res, execErr := repo.db.ExecContext(ctx,
		`UPDATE tags SET name = ?, color = ? WHERE id = ?`,
		tag.Name, nullableString(tag.Color), tag.ID.String(),
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

func (repo *TagRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, execErr := repo.db.ExecContext(ctx,
		`DELETE FROM tags WHERE id = ?`, id.String())

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

func (repo *TagRepo) CountTasksByTagID(ctx context.Context, id uuid.UUID) (int, error) {
	var count int
	err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tag_assignments WHERE tag_id = ?`, id.String(),
	).Scan(&count)
	return count, err
}

func (repo *TagRepo) ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error) {
	rows, err := repo.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.color, COUNT(ta.task_id)
		 FROM tags t
		 LEFT JOIN tag_assignments ta ON t.id = ta.tag_id
		 GROUP BY t.id, t.name, t.color`)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	result := make([]domain.TagWithUsage, 0)
	for rows.Next() {
		var (
			tw    domain.TagWithUsage
			rawID string
			color sql.NullString
		)
		if err := rows.Scan(&rawID, &tw.Tag.Name, &color, &tw.TaskCount); err != nil {
			return nil, err
		}
		parsed, err := uuid.Parse(rawID)

		if err != nil {
			return nil, err
		}

		tw.Tag.ID = parsed
		if color.Valid {
			tw.Tag.Color = &color.String
		}
		result = append(result, tw)
	}
	return result, rows.Err()
}

func (repo *TagRepo) List(ctx context.Context) ([]*domain.Tag, error) {
	rows, err := repo.db.QueryContext(ctx, `SELECT id, name, color FROM tags`)

	if err != nil {
		return nil, err
	}

	defer rows.Close()
	result := make([]*domain.Tag, 0)
	for rows.Next() {
		tag, err := scanTag(rows)

		if err != nil {
			return nil, err
		}

		result = append(result, tag)
	}
	return result, rows.Err()
}

func (repo *TagRepo) AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error {
	_, err := repo.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tag_assignments (task_id, tag_id) VALUES (?, ?)`,
		taskID.String(), tagID.String())
	return err
}

func (repo *TagRepo) RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error {
	res, execErr := repo.db.ExecContext(ctx,
		`DELETE FROM tag_assignments WHERE task_id = ? AND tag_id = ?`,
		taskID.String(), tagID.String())

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

func (repo *TagRepo) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := repo.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.color FROM tags t
		 JOIN tag_assignments ta ON t.id = ta.tag_id
		 WHERE ta.task_id = ?`, taskID.String())

	if err != nil {
		return nil, err
	}

	defer rows.Close()
	result := make([]*domain.Tag, 0)
	for rows.Next() {
		tag, err := scanTag(rows)

		if err != nil {
			return nil, err
		}

		result = append(result, tag)
	}
	return result, rows.Err()
}

// batchSize is the maximum number of placeholders per SQL IN clause,
// chosen to stay well under SQLite's default SQLITE_MAX_VARIABLE_NUMBER (999).
const tagBatchSize = 500

func (repo *TagRepo) GetTaskTagsBatch(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error) {
	result := make(map[uuid.UUID][]*domain.Tag, len(taskIDs))
	if len(taskIDs) == 0 {
		return result, nil
	}

	for start := 0; start < len(taskIDs); start += tagBatchSize {
		end := start + tagBatchSize
		if end > len(taskIDs) {
			end = len(taskIDs)
		}
		if err := repo.fetchTagBatch(ctx, taskIDs[start:end], result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (repo *TagRepo) fetchTagBatch(ctx context.Context, taskIDs []uuid.UUID, dest map[uuid.UUID][]*domain.Tag) error {
	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for index, id := range taskIDs {
		placeholders[index] = "?"
		args[index] = id.String()
	}

	query := `SELECT ta.task_id, t.id, t.name, t.color FROM tags t
		JOIN tag_assignments ta ON t.id = ta.tag_id
		WHERE ta.task_id IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := repo.db.QueryContext(ctx, query, args...)

	if err != nil {
		return err
	}

	defer rows.Close()

	for rows.Next() {
		var taskIDStr, tagIDStr string
		var tag domain.Tag
		var color sql.NullString
		if err := rows.Scan(&taskIDStr, &tagIDStr, &tag.Name, &color); err != nil {
			return err
		}
		taskID, err := uuid.Parse(taskIDStr)

		if err != nil {
			return err
		}

		tag.ID, err = uuid.Parse(tagIDStr)

		if err != nil {
			return err
		}

		if color.Valid {
			tag.Color = &color.String
		}
		dest[taskID] = append(dest[taskID], &tag)
	}
	return rows.Err()
}

type tagScanner interface {
	Scan(dest ...any) error
}

func scanTag(scanner tagScanner) (*domain.Tag, error) {
	var (
		tag   domain.Tag
		id    string
		color sql.NullString
	)
	if err := scanner.Scan(&id, &tag.Name, &color); err != nil {
		return nil, err
	}
	var err error
	tag.ID, err = uuid.Parse(id)

	if err != nil {
		return nil, err
	}

	if color.Valid {
		tag.Color = &color.String
	}
	return &tag, nil
}
