package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

const taskColumns = `id, short_id, parent_id, project_id, title, description,
	status, priority, version, due_at, wait_until, recurrence_rule, uda,
	created_at, modified_at`

type TaskRepo struct {
	db *sql.DB
}

func NewTaskRepo(db *sql.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) Create(ctx context.Context, task *domain.Task) error {
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO tasks (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskColumns),
		task.ID.String(), task.ShortID,
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
		task.Title, task.Description, task.Status, task.Priority, task.Version,
		nullableTime(task.DueAt), nullableTime(task.WaitUntil),
		nullableString(task.RecurrenceRule), marshalJSON(task.UDA),
		task.CreatedAt.UTC().Format(timeFormat),
		task.ModifiedAt.UTC().Format(timeFormat),
	)
	return err
}

func (r *TaskRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE id = ?`, taskColumns), id.String())
	return r.scanOne(row)
}

func (r *TaskRepo) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE short_id = ?`, taskColumns), shortID)
	return r.scanOne(row)
}

func (r *TaskRepo) Update(ctx context.Context, task *domain.Task) error {
	now := time.Now().UTC().Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `
		UPDATE tasks SET
			parent_id = ?, project_id = ?, title = ?, description = ?,
			status = ?, priority = ?, due_at = ?, wait_until = ?,
			recurrence_rule = ?, uda = ?, version = version + 1, modified_at = ?
		WHERE id = ? AND version = ?`,
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
		task.Title, task.Description, task.Status, task.Priority,
		nullableTime(task.DueAt), nullableTime(task.WaitUntil),
		nullableString(task.RecurrenceRule), marshalJSON(task.UDA),
		now, task.ID.String(), task.Version,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrConflict
	}
	task.Version++
	return nil
}

func (r *TaskRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tasks WHERE id = ? AND version = ?`, id.String(), version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrConflict
	}
	return nil
}

// Stubs — implemented in Phase 4 and Phase 5
func (r *TaskRepo) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
	return nil, fmt.Errorf("not implemented: see Phase 4")
}
func (r *TaskRepo) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	return nil, fmt.Errorf("not implemented: see Phase 5")
}
func (r *TaskRepo) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	return nil, fmt.Errorf("not implemented: see Phase 5")
}

func (r *TaskRepo) scanOne(row *sql.Row) (*domain.Task, error) {
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return t, err
}

func (r *TaskRepo) scanRows(rows *sql.Rows) ([]*domain.Task, error) {
	var result []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(s taskScanner) (*domain.Task, error) {
	var (
		t          domain.Task
		id         string
		parentID   sql.NullString
		projectID  sql.NullString
		dueAt      sql.NullString
		waitUntil  sql.NullString
		recurrence sql.NullString
		udaJSON    string
		createdAt  string
		modifiedAt string
	)
	err := s.Scan(
		&id, &t.ShortID, &parentID, &projectID,
		&t.Title, &t.Description, &t.Status, &t.Priority, &t.Version,
		&dueAt, &waitUntil, &recurrence, &udaJSON,
		&createdAt, &modifiedAt,
	)
	if err != nil {
		return nil, err
	}
	t.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parsing task ID: %w", err)
	}
	t.ParentID, err = parseUUID(parentID)
	if err != nil {
		return nil, fmt.Errorf("parsing parent_id: %w", err)
	}
	t.ProjectID, err = parseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("parsing project_id: %w", err)
	}
	t.DueAt, err = parseTime(dueAt)
	if err != nil {
		return nil, fmt.Errorf("parsing due_at: %w", err)
	}
	t.WaitUntil, err = parseTime(waitUntil)
	if err != nil {
		return nil, fmt.Errorf("parsing wait_until: %w", err)
	}
	if recurrence.Valid {
		t.RecurrenceRule = &recurrence.String
	}
	if err := json.Unmarshal([]byte(udaJSON), &t.UDA); err != nil {
		return nil, fmt.Errorf("parsing uda: %w", err)
	}
	t.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	t.ModifiedAt, err = time.Parse(timeFormat, modifiedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing modified_at: %w", err)
	}
	return &t, nil
}
