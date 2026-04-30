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

func (repo *TaskRepo) Create(ctx context.Context, task *domain.Task) error {
	udaJSON, udaErr := marshalJSON(task.UDA)

	if udaErr != nil {
		return udaErr
	}

	urgencyArg, urgencyErr := nullableUrgencyOverrides(task.UrgencyOverrides)

	if urgencyErr != nil {
		return urgencyErr
	}

	_, err := repo.db.ExecContext(ctx, fmt.Sprintf(
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

func (repo *TaskRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	row := repo.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE id = ?`, taskColumns), id.String())
	return repo.scanOne(row)
}

func (repo *TaskRepo) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	row := repo.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE short_id = ?`, taskColumns), shortID)
	return repo.scanOne(row)
}

func (repo *TaskRepo) Update(ctx context.Context, task *domain.Task) error {
	udaJSON, udaErr := marshalJSON(task.UDA)

	if udaErr != nil {
		return udaErr
	}

	urgencyArg, urgencyErr := nullableUrgencyOverrides(task.UrgencyOverrides)

	if urgencyErr != nil {
		return urgencyErr
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, execErr := repo.db.ExecContext(ctx, `
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

	if execErr != nil {
		return execErr
	}

	rowCount, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowCount == 0 {
		// Distinguish "task does not exist" from "version mismatch".
		var exists int
		lookupErr := repo.db.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, task.ID.String()).Scan(&exists)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}

	task.Version++
	task.ModifiedAt = now
	return nil
}

func (repo *TaskRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, execErr := repo.db.ExecContext(ctx,
		`DELETE FROM tasks WHERE id = ? AND version = ?`, id.String(), version)

	if execErr != nil {
		return execErr
	}

	rowCount, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowCount == 0 {
		var exists int
		lookupErr := repo.db.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, id.String()).Scan(&exists)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}

	return nil
}

// CountByProject returns how many tasks reference the given project.
func (repo *TaskRepo) CountByProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	var count int
	err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE project_id = ?`, projectID.String()).Scan(&count)

	if err != nil {
		return 0, err
	}

	return count, nil
}

// ReassignProject bulk-updates tasks.project_id from one project to another.
// Used by ProjectService.Delete under --force to migrate tasks off the project
// being removed so the FK on projects(id) does not fire. Does not bump version
// or modified_at — this is a migration operation, not a user mutation.
func (repo *TaskRepo) ReassignProject(ctx context.Context, fromID, toID uuid.UUID) (int, error) {
	res, execErr := repo.db.ExecContext(ctx,
		`UPDATE tasks SET project_id = ? WHERE project_id = ?`,
		toID.String(), fromID.String())

	if execErr != nil {
		return 0, execErr
	}

	rowCount, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return 0, rowsErr
	}

	return int(rowCount), nil
}

// List retrieves tasks matching the given filter expression. A nil filter
// returns all tasks.
func (repo *TaskRepo) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	where, args := buildFilterExpr(filter)
	query := fmt.Sprintf(`SELECT %s FROM tasks`, taskColumns)
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := repo.db.QueryContext(ctx, query, args...)

	if err != nil {
		return nil, err
	}

	defer rows.Close()
	return repo.scanRows(rows)
}

// GetChildren retrieves all direct children of the given parent task.
func (repo *TaskRepo) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	rows, err := repo.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE parent_id = ? ORDER BY "order" ASC NULLS LAST, created_at ASC, id ASC`, taskColumns),
		parentID.String(),
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()
	return repo.scanRows(rows)
}

// GetDescendants retrieves all descendants (children, grandchildren, etc.)
// of the given root task using a recursive CTE.
// SQLite's default SQLITE_MAX_RECURSIVE limit (1000) acts as a safety net
// against cycles or excessively deep hierarchies.
func (repo *TaskRepo) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	rows, err := repo.db.QueryContext(ctx, fmt.Sprintf(`
		WITH RECURSIVE descendants AS (
			SELECT %[1]s FROM tasks WHERE parent_id = ?
			UNION ALL
			SELECT %[2]s FROM tasks tk JOIN descendants sub ON tk.parent_id = sub.id
		)
		SELECT * FROM descendants ORDER BY "order" ASC NULLS LAST, created_at ASC, id ASC`, taskColumns, prefixColumns("tk", taskColumns)),
		rootID.String(),
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()
	return repo.scanRows(rows)
}

// GetAncestorOverrides returns every input task plus every ancestor reachable
// via parent_id, one row per visited node. Root nodes have a nil ParentID.
// Nodes without urgency_overrides have a nil Overrides pointer. Calling with
// a zero-length input returns an empty slice and no error.
func (repo *TaskRepo) GetAncestorOverrides(ctx context.Context, taskIDs []uuid.UUID) ([]repository.AncestorOverride, error) {
	if len(taskIDs) == 0 {
		return []repository.AncestorOverride{}, nil
	}
	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for index, id := range taskIDs {
		placeholders[index] = "?"
		args[index] = id.String()
	}
	query := fmt.Sprintf(`
		WITH RECURSIVE ancestors(id, parent_id, project_id, urgency_overrides) AS (
			SELECT id, parent_id, project_id, urgency_overrides
			FROM tasks
			WHERE id IN (%s)
			UNION
			SELECT tk.id, tk.parent_id, tk.project_id, tk.urgency_overrides
			FROM tasks tk
			INNER JOIN ancestors anc ON tk.id = anc.parent_id
		)
		SELECT id, parent_id, project_id, urgency_overrides FROM ancestors`,
		strings.Join(placeholders, ","))
	rows, queryErr := repo.db.QueryContext(ctx, query, args...)

	if queryErr != nil {
		return nil, queryErr
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
		if scanErr := rows.Scan(&idStr, &parentIDNull, &projectStr, &ovNull); scanErr != nil {
			return nil, scanErr
		}
		taskID, parseTaskErr := uuid.Parse(idStr)

		if parseTaskErr != nil {
			return nil, fmt.Errorf("parsing ancestor task id: %w", parseTaskErr)
		}

		parentID, parseParentErr := parseUUID(parentIDNull)

		if parseParentErr != nil {
			return nil, fmt.Errorf("parsing ancestor parent_id: %w", parseParentErr)
		}

		projectID, parseProjectErr := uuid.Parse(projectStr)

		if parseProjectErr != nil {
			return nil, fmt.Errorf("parsing ancestor project_id: %w", parseProjectErr)
		}

		var overrides *domain.UrgencyOverrides
		if ovNull.Valid {
			var override domain.UrgencyOverrides
			if unmarshalErr := json.Unmarshal([]byte(ovNull.String), &override); unmarshalErr != nil {
				return nil, fmt.Errorf("scanning task %s: decoding urgency_overrides: %w", idStr, unmarshalErr)
			}
			overrides = &override
		}
		result = append(result, repository.AncestorOverride{
			TaskID:    taskID,
			ParentID:  parentID,
			ProjectID: projectID,
			Overrides: overrides,
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	return result, nil
}

// NextOrder returns max("order") + 1.0 for siblings under parentID. parentID == nil
// scopes to root-level siblings. Returns 1.0 when the group is empty.
func (repo *TaskRepo) NextOrder(ctx context.Context, parentID *uuid.UUID) (float64, error) {
	var maxOrder sql.NullFloat64
	var err error
	if parentID == nil {
		err = repo.db.QueryRowContext(ctx,
			`SELECT MAX("order") FROM tasks WHERE parent_id IS NULL`).Scan(&maxOrder)
	} else {
		err = repo.db.QueryRowContext(ctx,
			`SELECT MAX("order") FROM tasks WHERE parent_id = ?`, parentID.String()).Scan(&maxOrder)
	}
	if err != nil {
		return 0, err
	}
	if !maxOrder.Valid {
		return 1.0, nil
	}
	return maxOrder.Float64 + 1.0, nil
}

// FirstOrder returns min("order") - 1.0 for siblings under parentID. parentID == nil
// scopes to root-level siblings. Returns 1.0 when the group is empty.
func (repo *TaskRepo) FirstOrder(ctx context.Context, parentID *uuid.UUID) (float64, error) {
	var minOrder sql.NullFloat64
	var err error
	if parentID == nil {
		err = repo.db.QueryRowContext(ctx,
			`SELECT MIN("order") FROM tasks WHERE parent_id IS NULL`).Scan(&minOrder)
	} else {
		err = repo.db.QueryRowContext(ctx,
			`SELECT MIN("order") FROM tasks WHERE parent_id = ?`, parentID.String()).Scan(&minOrder)
	}
	if err != nil {
		return 0, err
	}
	if !minOrder.Valid {
		return 1.0, nil
	}
	return minOrder.Float64 - 1.0, nil
}

// UpdateOrderAndParent runs the narrow move statement: parent_id, "order",
// version (+1), and modified_at for a single row under an optimistic-lock
// check. It intentionally leaves every other column untouched so Move and
// Resequence cannot clobber fields they never loaded.
func (repo *TaskRepo) UpdateOrderAndParent(
	ctx context.Context,
	id uuid.UUID,
	parentID *uuid.UUID,
	order float64,
	fromVersion int,
	updatedAt time.Time,
) (int, error) {
	res, execErr := repo.db.ExecContext(ctx, `
		UPDATE tasks SET parent_id = ?, "order" = ?, version = version + 1, modified_at = ?
		WHERE id = ? AND version = ?`,
		nullableUUID(parentID), order,
		updatedAt.UTC().Format(timeFormat),
		id.String(), fromVersion,
	)

	if execErr != nil {
		return 0, execErr
	}

	rowCount, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return 0, rowsErr
	}

	if rowCount == 0 {
		var exists int
		lookupErr := repo.db.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, id.String()).Scan(&exists)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return 0, domain.ErrNotFound
		}
		return 0, domain.ErrConflict
	}

	return fromVersion + 1, nil
}

// NeighborOrders returns the nearest ordered neighbors of pivot within the sibling
// group under parentID. prev is the largest order < pivot (nil if none); next is
// the smallest order > pivot (nil if none). parentID == nil scopes to root.
func (repo *TaskRepo) NeighborOrders(ctx context.Context, parentID *uuid.UUID, pivot float64) (prev, next *float64, err error) {
	var (
		prevN sql.NullFloat64
		nextN sql.NullFloat64
	)
	if parentID == nil {
		err = repo.db.QueryRowContext(ctx, `
			SELECT
				(SELECT "order" FROM tasks WHERE parent_id IS NULL AND "order" < ? ORDER BY "order" DESC LIMIT 1),
				(SELECT "order" FROM tasks WHERE parent_id IS NULL AND "order" > ? ORDER BY "order" ASC LIMIT 1)
		`, pivot, pivot).Scan(&prevN, &nextN)
	} else {
		pid := parentID.String()
		err = repo.db.QueryRowContext(ctx, `
			SELECT
				(SELECT "order" FROM tasks WHERE parent_id = ? AND "order" < ? ORDER BY "order" DESC LIMIT 1),
				(SELECT "order" FROM tasks WHERE parent_id = ? AND "order" > ? ORDER BY "order" ASC LIMIT 1)
		`, pid, pivot, pid, pivot).Scan(&prevN, &nextN)
	}
	if err != nil {
		return nil, nil, err
	}
	if prevN.Valid {
		prevVal := prevN.Float64
		prev = &prevVal
	}
	if nextN.Valid {
		nextVal := nextN.Float64
		next = &nextVal
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
				SELECT tk.id FROM tasks tk JOIN descendants sub ON tk.parent_id = sub.id
			)
			SELECT id FROM descendants
		)`)
		args = append(args, filter.RootID.String())
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for index, status := range filter.Statuses {
			placeholders[index] = "?"
			args = append(args, status)
		}
		conditions = append(conditions, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(filter.Levels) > 0 {
		placeholders := make([]string, len(filter.Levels))
		for index, level := range filter.Levels {
			placeholders[index] = "?"
			args = append(args, level)
		}
		conditions = append(conditions, fmt.Sprintf("level IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(filter.Tags) > 0 {
		placeholders := make([]string, len(filter.Tags))
		for index, tag := range filter.Tags {
			placeholders[index] = "?"
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
		for index, tag := range filter.ExcludeTags {
			placeholders[index] = "?"
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
		for key := range filter.UDA {
			udaKeys = append(udaKeys, key)
		}
		sort.Strings(udaKeys)
		for _, key := range udaKeys {
			udaValue := filter.UDA[key]
			jsonPath := "$." + key
			if udaValue == "" {
				// Empty value = key absent or empty string
				conditions = append(conditions, "(json_extract(uda, ?) IS NULL OR json_extract(uda, ?) = '')")
				args = append(args, jsonPath, jsonPath)
			} else {
				conditions = append(conditions, "json_extract(uda, ?) = ?")
				args = append(args, jsonPath, udaValue)
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

	switch filterNode := expr.(type) {
	case *domain.TermFilter:
		return buildFilter(filterNode.TaskFilter)

	case *domain.AndFilter:
		var conditions []string
		for _, child := range filterNode.Children {
			clause, clauseArgs := buildFilterExpr(child)
			if clause != "" {
				conditions = append(conditions, clause)
				args = append(args, clauseArgs...)
			}
		}
		if len(conditions) == 0 {
			return "", args
		}
		return "(" + strings.Join(conditions, " AND ") + ")", args

	case *domain.OrFilter:
		var conditions []string
		for _, child := range filterNode.Children {
			clause, clauseArgs := buildFilterExpr(child)
			if clause != "" {
				conditions = append(conditions, clause)
				args = append(args, clauseArgs...)
			}
		}
		if len(conditions) == 0 {
			return "", args
		}
		return "(" + strings.Join(conditions, " OR ") + ")", args

	case *domain.NotFilter:
		clause, clauseArgs := buildFilterExpr(filterNode.Child)
		if clause == "" {
			return "", clauseArgs
		}
		return "NOT (" + clause + ")", clauseArgs

	default:
		return "", nil
	}
}

func (repo *TaskRepo) scanOne(row *sql.Row) (*domain.Task, error) {
	task, err := scanTask(row)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}

	return task, err
}

func (repo *TaskRepo) scanRows(rows *sql.Rows) ([]*domain.Task, error) {
	result := make([]*domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)

		if err != nil {
			return nil, err
		}

		result = append(result, task)
	}
	return result, rows.Err()
}

// prefixColumns adds a table alias prefix to each column name.
// e.g. prefixColumns("t", "id, name") -> "t.id, t.name"
func prefixColumns(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for index, part := range parts {
		parts[index] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (*domain.Task, error) {
	var (
		result           domain.Task
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
	scanErr := scanner.Scan(
		&id, &result.ShortID, &parentID, &projectID,
		&result.Title, &result.Description, &result.Status, &result.Priority, &order, &result.Version,
		&dueAt, &waitUntil, &recurrence, &udaJSON, &urgencyOverrides,
		&createdAt, &modifiedAt, &claimedBy, &claimedAt, &level,
	)

	if scanErr != nil {
		return nil, scanErr
	}

	var parseErr error
	result.ID, parseErr = uuid.Parse(id)

	if parseErr != nil {
		return nil, fmt.Errorf("parsing task ID: %w", parseErr)
	}

	result.ParentID, parseErr = parseUUID(parentID)

	if parseErr != nil {
		return nil, fmt.Errorf("parsing parent_id: %w", parseErr)
	}

	result.ProjectID, parseErr = uuid.Parse(projectID)

	if parseErr != nil {
		return nil, fmt.Errorf("parsing task.project_id: %w", parseErr)
	}

	if order.Valid {
		orderVal := order.Float64
		result.Order = &orderVal
	}
	result.DueAt, parseErr = parseTime(dueAt)

	if parseErr != nil {
		return nil, fmt.Errorf("parsing due_at: %w", parseErr)
	}

	result.WaitUntil, parseErr = parseTime(waitUntil)

	if parseErr != nil {
		return nil, fmt.Errorf("parsing wait_until: %w", parseErr)
	}

	if recurrence.Valid {
		result.RecurrenceRule = &recurrence.String
	}
	if udaErr := json.Unmarshal([]byte(udaJSON), &result.UDA); udaErr != nil {
		return nil, fmt.Errorf("parsing uda: %w", udaErr)
	}
	if urgencyOverrides.Valid {
		var override domain.UrgencyOverrides
		if urgencyErr := json.Unmarshal([]byte(urgencyOverrides.String), &override); urgencyErr != nil {
			return nil, fmt.Errorf("scanning task %s: decoding urgency_overrides: %w", id, urgencyErr)
		}
		result.UrgencyOverrides = &override
	}
	result.CreatedAt, parseErr = time.Parse(timeFormat, createdAt)

	if parseErr != nil {
		return nil, fmt.Errorf("parsing created_at: %w", parseErr)
	}

	result.ModifiedAt, parseErr = time.Parse(timeFormat, modifiedAt)

	if parseErr != nil {
		return nil, fmt.Errorf("parsing modified_at: %w", parseErr)
	}

	if claimedBy.Valid {
		result.ClaimedBy = &claimedBy.String
	}
	result.ClaimedAt, parseErr = parseTime(claimedAt)

	if parseErr != nil {
		return nil, fmt.Errorf("parsing claimed_at: %w", parseErr)
	}

	if level.Valid {
		result.Level = &level.String
	}
	return &result, nil
}
