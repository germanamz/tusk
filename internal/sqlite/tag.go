package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type TagRepo struct {
	db *sql.DB
}

func NewTagRepo(db *sql.DB) *TagRepo {
	return &TagRepo{db: db}
}

func (r *TagRepo) Create(ctx context.Context, tag *domain.Tag) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tags (id, name, color) VALUES (?, ?, ?)`,
		tag.ID.String(), tag.Name, nullableString(tag.Color),
	)
	return err
}

func (r *TagRepo) GetByName(ctx context.Context, name string) (*domain.Tag, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, color FROM tags WHERE name = ?`, name)
	tag, err := scanTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return tag, err
}

func (r *TagRepo) List(ctx context.Context) ([]*domain.Tag, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, color FROM tags`)
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

func (r *TagRepo) AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tag_assignments (task_id, tag_id) VALUES (?, ?)`,
		taskID.String(), tagID.String())
	return err
}

func (r *TagRepo) RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tag_assignments WHERE task_id = ? AND tag_id = ?`,
		taskID.String(), tagID.String())
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

func (r *TagRepo) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.QueryContext(ctx,
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

type tagScanner interface {
	Scan(dest ...any) error
}

func scanTag(s tagScanner) (*domain.Tag, error) {
	var (
		tag   domain.Tag
		id    string
		color sql.NullString
	)
	if err := s.Scan(&id, &tag.Name, &color); err != nil {
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
