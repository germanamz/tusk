package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	udaJSON, err := marshalJSON(task.UDA)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO tasks (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskColumns),
		task.ID.String(), task.ShortID,
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
		task.Title, task.Description, task.Status, task.Priority, task.Version,
		nullableTime(task.DueAt), nullableTime(task.WaitUntil),
		nullableString(task.RecurrenceRule), udaJSON,
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
	udaJSON, err := marshalJSON(task.UDA)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `
		UPDATE tasks SET
			parent_id = ?, project_id = ?, title = ?, description = ?,
			status = ?, priority = ?, due_at = ?, wait_until = ?,
			recurrence_rule = ?, uda = ?, version = version + 1, modified_at = ?
		WHERE id = ? AND version = ?`,
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
		task.Title, task.Description, task.Status, task.Priority,
		nullableTime(task.DueAt), nullableTime(task.WaitUntil),
		nullableString(task.RecurrenceRule), udaJSON,
		nowStr, task.ID.String(), task.Version,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Distinguish "task does not exist" from "version mismatch".
		var exists int
		err := r.db.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, task.ID.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	task.Version++
	task.ModifiedAt = now
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
		var exists int
		err := r.db.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, id.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	return nil
}

// List retrieves tasks matching the given filter. An empty filter returns all
// tasks. The filter fields are combined with AND logic — a task must match
// every non-nil/non-empty filter field to be included.
func (r *TaskRepo) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
	ctePrefix, where, args := buildFilter(filter)
	query := ctePrefix + fmt.Sprintf(`SELECT %s FROM tasks`, taskColumns)
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// GetChildren retrieves all direct children of the given parent task.
func (r *TaskRepo) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE parent_id = ?`, taskColumns),
		parentID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// GetDescendants retrieves all descendants (children, grandchildren, etc.)
// of the given root task using a recursive CTE.
// SQLite's default SQLITE_MAX_RECURSIVE limit (1000) acts as a safety net
// against cycles or excessively deep hierarchies.
func (r *TaskRepo) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		WITH RECURSIVE descendants AS (
			SELECT %[1]s FROM tasks WHERE parent_id = ?
			UNION ALL
			SELECT %[2]s FROM tasks t JOIN descendants d ON t.parent_id = d.id
		)
		SELECT * FROM descendants`, taskColumns, prefixColumns("t", taskColumns)),
		rootID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// buildFilter translates a TaskFilter struct into SQL fragments:
//   - ctePrefix: a WITH RECURSIVE clause (only set when RootID is used)
//   - where: the WHERE clause body (conditions joined by AND)
//   - args: the parameter values corresponding to ? placeholders
func buildFilter(filter domain.TaskFilter) (ctePrefix string, where string, args []any) {
	var conditions []string

	if filter.ProjectID != nil {
		conditions = append(conditions, "project_id = ?")
		args = append(args, filter.ProjectID.String())
	}
	if filter.ParentID != nil {
		conditions = append(conditions, "parent_id = ?")
		args = append(args, filter.ParentID.String())
	}
	if filter.RootID != nil {
		ctePrefix = `WITH RECURSIVE descendants(id) AS (
			SELECT id FROM tasks WHERE parent_id = ?
			UNION ALL
			SELECT t.id FROM tasks t JOIN descendants d ON t.parent_id = d.id
		) `
		args = append([]any{filter.RootID.String()}, args...)
		conditions = append(conditions, "tasks.id IN (SELECT id FROM descendants)")
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, s)
		}
		conditions = append(conditions, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(filter.Tags) > 0 {
		placeholders := make([]string, len(filter.Tags))
		for i, tag := range filter.Tags {
			placeholders[i] = "?"
			args = append(args, tag)
		}
		conditions = append(conditions, fmt.Sprintf(
			`(SELECT COUNT(DISTINCT tg.name) FROM tag_assignments ta
			  JOIN tags tg ON ta.tag_id = tg.id
			  WHERE ta.task_id = tasks.id AND tg.name IN (%s)) = ?`,
			strings.Join(placeholders, ",")))
		args = append(args, len(filter.Tags))
	}
	if len(filter.ExcludeTags) > 0 {
		placeholders := make([]string, len(filter.ExcludeTags))
		for i, tag := range filter.ExcludeTags {
			placeholders[i] = "?"
			args = append(args, tag)
		}
		conditions = append(conditions, fmt.Sprintf(
			`NOT EXISTS (SELECT 1 FROM tag_assignments ta
			 JOIN tags tg ON ta.tag_id = tg.id
			 WHERE ta.task_id = tasks.id AND tg.name IN (%s))`,
			strings.Join(placeholders, ",")))
	}
	if filter.PriorityMin != nil {
		conditions = append(conditions, "priority >= ?")
		args = append(args, *filter.PriorityMin)
	}
	if filter.PriorityMax != nil {
		conditions = append(conditions, "priority <= ?")
		args = append(args, *filter.PriorityMax)
	}
	if filter.DueAfter != nil {
		conditions = append(conditions, "due_at > ?")
		args = append(args, filter.DueAfter.UTC().Format(timeFormat))
	}
	if filter.DueBefore != nil {
		conditions = append(conditions, "due_at < ?")
		args = append(args, filter.DueBefore.UTC().Format(timeFormat))
	}
	if filter.WaitingOnly != nil && *filter.WaitingOnly {
		conditions = append(conditions, "wait_until > ?")
		args = append(args, time.Now().UTC().Format(timeFormat))
	}
	return ctePrefix, strings.Join(conditions, " AND "), args
}

func (r *TaskRepo) scanOne(row *sql.Row) (*domain.Task, error) {
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return t, err
}

func (r *TaskRepo) scanRows(rows *sql.Rows) ([]*domain.Task, error) {
	result := make([]*domain.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// prefixColumns adds a table alias prefix to each column name.
// e.g. prefixColumns("t", "id, name") -> "t.id, t.name"
func prefixColumns(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
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
