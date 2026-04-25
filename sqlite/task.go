package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

const taskColumns = `id, short_id, parent_id, project_id, title, description,
	status, priority, "order", version, due_at, wait_until, recurrence_rule, uda,
	urgency_overrides, created_at, modified_at, claimed_by, claimed_at, level`

type TaskRepo struct {
	db DBTX
}

func NewTaskRepo(db DBTX) *TaskRepo {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) Create(ctx context.Context, task *domain.Task) error {
	udaJSON, err := marshalJSON(task.UDA)
	if err != nil {
		return err
	}
	urgencyArg, err := nullableUrgencyOverrides(task.UrgencyOverrides)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO tasks (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskColumns),
		task.ID.String(), task.ShortID,
		nullableUUID(task.ParentID), task.ProjectID.String(),
		task.Title, task.Description, task.Status, task.Priority,
		nullableFloat(task.Order),
		task.Version,
		nullableTime(task.DueAt), nullableTime(task.WaitUntil),
		nullableString(task.RecurrenceRule), udaJSON,
		urgencyArg,
		task.CreatedAt.UTC().Format(timeFormat),
		task.ModifiedAt.UTC().Format(timeFormat),
		nullableString(task.ClaimedBy), nullableTime(task.ClaimedAt),
		nullableString(task.Level),
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
	urgencyArg, err := nullableUrgencyOverrides(task.UrgencyOverrides)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `
		UPDATE tasks SET
			parent_id = ?, project_id = ?, title = ?, description = ?,
			status = ?, priority = ?, "order" = ?, due_at = ?, wait_until = ?,
			recurrence_rule = ?, uda = ?, urgency_overrides = ?, version = version + 1, modified_at = ?,
			claimed_by = ?, claimed_at = ?, level = ?
		WHERE id = ? AND version = ?`,
		nullableUUID(task.ParentID), task.ProjectID.String(),
		task.Title, task.Description, task.Status, task.Priority,
		nullableFloat(task.Order),
		nullableTime(task.DueAt), nullableTime(task.WaitUntil),
		nullableString(task.RecurrenceRule), udaJSON,
		urgencyArg,
		nowStr, nullableString(task.ClaimedBy), nullableTime(task.ClaimedAt),
		nullableString(task.Level),
		task.ID.String(), task.Version,
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

// CountByProject returns how many tasks reference the given project.
func (r *TaskRepo) CountByProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE project_id = ?`, projectID.String()).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ReassignProject bulk-updates tasks.project_id from one project to another.
// Used by ProjectService.Delete under --force to migrate tasks off the project
// being removed so the FK on projects(id) does not fire. Does not bump version
// or modified_at — this is a migration operation, not a user mutation.
func (r *TaskRepo) ReassignProject(ctx context.Context, fromID, toID uuid.UUID) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET project_id = ? WHERE project_id = ?`,
		toID.String(), fromID.String())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// List retrieves tasks matching the given filter expression. A nil filter
// returns all tasks.
func (r *TaskRepo) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	where, args := buildFilterExpr(filter)
	query := fmt.Sprintf(`SELECT %s FROM tasks`, taskColumns)
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
		fmt.Sprintf(`SELECT %s FROM tasks WHERE parent_id = ? ORDER BY "order" ASC NULLS LAST, created_at ASC, id ASC`, taskColumns),
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
		SELECT * FROM descendants ORDER BY "order" ASC NULLS LAST, created_at ASC, id ASC`, taskColumns, prefixColumns("t", taskColumns)),
		rootID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// GetAncestorOverrides returns every input task plus every ancestor reachable
// via parent_id, one row per visited node. Root nodes have a nil ParentID.
// Nodes without urgency_overrides have a nil Overrides pointer. Calling with
// a zero-length input returns an empty slice and no error.
func (r *TaskRepo) GetAncestorOverrides(ctx context.Context, taskIDs []uuid.UUID) ([]repository.AncestorOverride, error) {
	if len(taskIDs) == 0 {
		return []repository.AncestorOverride{}, nil
	}
	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		placeholders[i] = "?"
		args[i] = id.String()
	}
	query := fmt.Sprintf(`
		WITH RECURSIVE ancestors(id, parent_id, project_id, urgency_overrides) AS (
			SELECT id, parent_id, project_id, urgency_overrides
			FROM tasks
			WHERE id IN (%s)
			UNION
			SELECT t.id, t.parent_id, t.project_id, t.urgency_overrides
			FROM tasks t
			INNER JOIN ancestors a ON t.id = a.parent_id
		)
		SELECT id, parent_id, project_id, urgency_overrides FROM ancestors`,
		strings.Join(placeholders, ","))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]repository.AncestorOverride, 0)
	for rows.Next() {
		var (
			idStr        string
			parentIDNull sql.NullString
			projectStr   string
			ovNull       sql.NullString
		)
		if err := rows.Scan(&idStr, &parentIDNull, &projectStr, &ovNull); err != nil {
			return nil, err
		}
		taskID, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parsing ancestor task id: %w", err)
		}
		parentID, err := parseUUID(parentIDNull)
		if err != nil {
			return nil, fmt.Errorf("parsing ancestor parent_id: %w", err)
		}
		projectID, err := uuid.Parse(projectStr)
		if err != nil {
			return nil, fmt.Errorf("parsing ancestor project_id: %w", err)
		}
		var overrides *domain.UrgencyOverrides
		if ovNull.Valid {
			var ov domain.UrgencyOverrides
			if err := json.Unmarshal([]byte(ovNull.String), &ov); err != nil {
				return nil, fmt.Errorf("scanning task %s: decoding urgency_overrides: %w", idStr, err)
			}
			overrides = &ov
		}
		result = append(result, repository.AncestorOverride{
			TaskID:    taskID,
			ParentID:  parentID,
			ProjectID: projectID,
			Overrides: overrides,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// NextOrder returns max("order") + 1.0 for siblings under parentID. parentID == nil
// scopes to root-level siblings. Returns 1.0 when the group is empty.
func (r *TaskRepo) NextOrder(ctx context.Context, parentID *uuid.UUID) (float64, error) {
	var max sql.NullFloat64
	var err error
	if parentID == nil {
		err = r.db.QueryRowContext(ctx,
			`SELECT MAX("order") FROM tasks WHERE parent_id IS NULL`).Scan(&max)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT MAX("order") FROM tasks WHERE parent_id = ?`, parentID.String()).Scan(&max)
	}
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 1.0, nil
	}
	return max.Float64 + 1.0, nil
}

// FirstOrder returns min("order") - 1.0 for siblings under parentID. parentID == nil
// scopes to root-level siblings. Returns 1.0 when the group is empty.
func (r *TaskRepo) FirstOrder(ctx context.Context, parentID *uuid.UUID) (float64, error) {
	var min sql.NullFloat64
	var err error
	if parentID == nil {
		err = r.db.QueryRowContext(ctx,
			`SELECT MIN("order") FROM tasks WHERE parent_id IS NULL`).Scan(&min)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT MIN("order") FROM tasks WHERE parent_id = ?`, parentID.String()).Scan(&min)
	}
	if err != nil {
		return 0, err
	}
	if !min.Valid {
		return 1.0, nil
	}
	return min.Float64 - 1.0, nil
}

// UpdateOrderAndParent runs the narrow move statement: parent_id, "order",
// version (+1), and modified_at for a single row under an optimistic-lock
// check. It intentionally leaves every other column untouched so Move and
// Resequence cannot clobber fields they never loaded.
func (r *TaskRepo) UpdateOrderAndParent(
	ctx context.Context,
	id uuid.UUID,
	parentID *uuid.UUID,
	order float64,
	fromVersion int,
	updatedAt time.Time,
) (int, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE tasks SET parent_id = ?, "order" = ?, version = version + 1, modified_at = ?
		WHERE id = ? AND version = ?`,
		nullableUUID(parentID), order,
		updatedAt.UTC().Format(timeFormat),
		id.String(), fromVersion,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		var exists int
		err := r.db.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, id.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, domain.ErrNotFound
		}
		return 0, domain.ErrConflict
	}
	return fromVersion + 1, nil
}

// NeighborOrders returns the nearest ordered neighbors of pivot within the sibling
// group under parentID. prev is the largest order < pivot (nil if none); next is
// the smallest order > pivot (nil if none). parentID == nil scopes to root.
func (r *TaskRepo) NeighborOrders(ctx context.Context, parentID *uuid.UUID, pivot float64) (prev, next *float64, err error) {
	var (
		prevN sql.NullFloat64
		nextN sql.NullFloat64
	)
	if parentID == nil {
		err = r.db.QueryRowContext(ctx, `
			SELECT
				(SELECT "order" FROM tasks WHERE parent_id IS NULL AND "order" < ? ORDER BY "order" DESC LIMIT 1),
				(SELECT "order" FROM tasks WHERE parent_id IS NULL AND "order" > ? ORDER BY "order" ASC LIMIT 1)
		`, pivot, pivot).Scan(&prevN, &nextN)
	} else {
		pid := parentID.String()
		err = r.db.QueryRowContext(ctx, `
			SELECT
				(SELECT "order" FROM tasks WHERE parent_id = ? AND "order" < ? ORDER BY "order" DESC LIMIT 1),
				(SELECT "order" FROM tasks WHERE parent_id = ? AND "order" > ? ORDER BY "order" ASC LIMIT 1)
		`, pid, pivot, pid, pivot).Scan(&prevN, &nextN)
	}
	if err != nil {
		return nil, nil, err
	}
	if prevN.Valid {
		v := prevN.Float64
		prev = &v
	}
	if nextN.Valid {
		v := nextN.Float64
		next = &v
	}
	return prev, next, nil
}

// buildFilter translates a TaskFilter struct into a WHERE clause body and args.
func buildFilter(filter domain.TaskFilter) (where string, args []any) {
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
		conditions = append(conditions, `tasks.id IN (
			WITH RECURSIVE descendants(id) AS (
				SELECT id FROM tasks WHERE parent_id = ?
				UNION ALL
				SELECT t.id FROM tasks t JOIN descendants d ON t.parent_id = d.id
			)
			SELECT id FROM descendants
		)`)
		args = append(args, filter.RootID.String())
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, s)
		}
		conditions = append(conditions, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(filter.Levels) > 0 {
		placeholders := make([]string, len(filter.Levels))
		for i, lvl := range filter.Levels {
			placeholders[i] = "?"
			args = append(args, lvl)
		}
		conditions = append(conditions, fmt.Sprintf("level IN (%s)", strings.Join(placeholders, ",")))
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
	if filter.OrderIsNull != nil && *filter.OrderIsNull {
		conditions = append(conditions, `"order" IS NULL`)
	}
	if filter.OrderMin != nil {
		conditions = append(conditions, `"order" >= ?`)
		args = append(args, *filter.OrderMin)
	}
	if filter.OrderMax != nil {
		conditions = append(conditions, `"order" <= ?`)
		args = append(args, *filter.OrderMax)
	}
	if filter.DueAfter != nil {
		conditions = append(conditions, "due_at >= ?")
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
	if filter.TitleContains != nil {
		conditions = append(conditions, "LOWER(title) LIKE '%' || LOWER(?) || '%'")
		args = append(args, *filter.TitleContains)
	}
	if filter.DescriptionContains != nil {
		conditions = append(conditions, "LOWER(description) LIKE '%' || LOWER(?) || '%'")
		args = append(args, *filter.DescriptionContains)
	}
	if filter.ClaimedBy != nil {
		conditions = append(conditions, "claimed_by = ?")
		args = append(args, *filter.ClaimedBy)
	}
	if filter.Unclaimed != nil && *filter.Unclaimed {
		conditions = append(conditions, "claimed_by IS NULL")
	}
	if len(filter.UDA) > 0 {
		// Sort keys for deterministic query generation (important for tests)
		udaKeys := make([]string, 0, len(filter.UDA))
		for k := range filter.UDA {
			udaKeys = append(udaKeys, k)
		}
		sort.Strings(udaKeys)
		for _, k := range udaKeys {
			v := filter.UDA[k]
			jsonPath := "$." + k
			if v == "" {
				// Empty value = key absent or empty string
				conditions = append(conditions, "(json_extract(uda, ?) IS NULL OR json_extract(uda, ?) = '')")
				args = append(args, jsonPath, jsonPath)
			} else {
				conditions = append(conditions, "json_extract(uda, ?) = ?")
				args = append(args, jsonPath, v)
			}
		}
	}
	return strings.Join(conditions, " AND "), args
}

// buildFilterExpr recursively translates a domain.FilterExpr tree into SQL.
func buildFilterExpr(expr domain.FilterExpr) (where string, args []any) {
	if expr == nil {
		return "", nil
	}

	switch e := expr.(type) {
	case *domain.TermFilter:
		return buildFilter(e.TaskFilter)

	case *domain.AndFilter:
		var conditions []string
		for _, child := range e.Children {
			w, a := buildFilterExpr(child)
			if w != "" {
				conditions = append(conditions, w)
				args = append(args, a...)
			}
		}
		if len(conditions) == 0 {
			return "", args
		}
		return "(" + strings.Join(conditions, " AND ") + ")", args

	case *domain.OrFilter:
		var conditions []string
		for _, child := range e.Children {
			w, a := buildFilterExpr(child)
			if w != "" {
				conditions = append(conditions, w)
				args = append(args, a...)
			}
		}
		if len(conditions) == 0 {
			return "", args
		}
		return "(" + strings.Join(conditions, " OR ") + ")", args

	case *domain.NotFilter:
		w, a := buildFilterExpr(e.Child)
		if w == "" {
			return "", a
		}
		return "NOT (" + w + ")", a

	default:
		return "", nil
	}
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
		t                domain.Task
		id               string
		parentID         sql.NullString
		projectID        string
		order            sql.NullFloat64
		dueAt            sql.NullString
		waitUntil        sql.NullString
		recurrence       sql.NullString
		udaJSON          string
		urgencyOverrides sql.NullString
		createdAt        string
		modifiedAt       string
		claimedBy        sql.NullString
		claimedAt        sql.NullString
		level            sql.NullString
	)
	err := s.Scan(
		&id, &t.ShortID, &parentID, &projectID,
		&t.Title, &t.Description, &t.Status, &t.Priority, &order, &t.Version,
		&dueAt, &waitUntil, &recurrence, &udaJSON, &urgencyOverrides,
		&createdAt, &modifiedAt, &claimedBy, &claimedAt, &level,
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
	t.ProjectID, err = uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("parsing task.project_id: %w", err)
	}
	if order.Valid {
		v := order.Float64
		t.Order = &v
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
	if urgencyOverrides.Valid {
		var ov domain.UrgencyOverrides
		if err := json.Unmarshal([]byte(urgencyOverrides.String), &ov); err != nil {
			return nil, fmt.Errorf("scanning task %s: decoding urgency_overrides: %w", id, err)
		}
		t.UrgencyOverrides = &ov
	}
	t.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	t.ModifiedAt, err = time.Parse(timeFormat, modifiedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing modified_at: %w", err)
	}
	if claimedBy.Valid {
		t.ClaimedBy = &claimedBy.String
	}
	t.ClaimedAt, err = parseTime(claimedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing claimed_at: %w", err)
	}
	if level.Valid {
		t.Level = &level.String
	}
	return &t, nil
}
