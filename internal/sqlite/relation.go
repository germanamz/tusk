package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// relationColumns lists the columns selected in every relation query.
// Having this as a constant means if we ever add or remove a column, we change
// it in ONE place instead of in every SQL string. The order must match what
// scanRelations expects in its Scan call.
const relationColumns = `id, source_id, target_id, relation_type, created_at`

// RelationRepo implements repository.RelationRepository using SQLite.
//
// It manages directed relationships between tasks. Each relation has a source
// task and a target task, plus a type ("blocks", "relates_to", "duplicates").
// The direction matters: "A blocks B" is different from "B blocks A".
type RelationRepo struct {
	db DBTX
}

// NewRelationRepo creates a RelationRepo. Pass in the *sql.DB from Store.DB().
func NewRelationRepo(db DBTX) *RelationRepo {
	return &RelationRepo{db: db}
}

// Create inserts a new relation into the database.
//
// The caller must set all fields on the Relation struct before calling Create.
// Both SourceID and TargetID must reference existing tasks (foreign key constraint).
//
// If a relation with the same (source_id, target_id, relation_type) already exists,
// the UNIQUE constraint fires and this method returns domain.ErrDuplicateRelation.
func (r *RelationRepo) Create(ctx context.Context, rel *domain.Relation) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO relations (id, source_id, target_id, relation_type, created_at) VALUES (?, ?, ?, ?, ?)`,
		rel.ID.String(), rel.SourceID.String(), rel.TargetID.String(),
		rel.RelationType, rel.CreatedAt.UTC().Format(timeFormat),
	)
	if err != nil && isUniqueViolation(err) {
		return domain.ErrDuplicateRelation
	}
	return err
}

// Delete removes a relation by its ID.
// Returns domain.ErrNotFound if no relation with that ID exists.
//
// Uses the same RowsAffected pattern as AnnotationRepo.Delete.
func (r *RelationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM relations WHERE id = ?`, id.String())
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

// DeleteByFields removes a relation matching the exact (source, target, type) triple.
// Returns domain.ErrNotFound if no such relation exists.
func (r *RelationRepo) DeleteByFields(ctx context.Context, sourceID, targetID uuid.UUID, relType string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM relations WHERE source_id = ? AND target_id = ? AND relation_type = ?`,
		sourceID.String(), targetID.String(), relType)
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

// GetByTask retrieves ALL relations where the given task is involved, regardless
// of whether it is the source or the target.
//
// The SQL uses OR: WHERE source_id = ? OR target_id = ?
func (r *RelationRepo) GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations WHERE source_id = ? OR target_id = ?`,
		taskID.String(), taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

// GetBlocking retrieves relations where the given task is the SOURCE and the
// relation type is "blocks". In other words: "what tasks does this task block?"
func (r *RelationRepo) GetBlocking(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations WHERE source_id = ? AND relation_type = 'blocks'`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

// GetBlockedBy retrieves relations where the given task is the TARGET and the
// relation type is "blocks". In other words: "what tasks are blocking this task?"
func (r *RelationRepo) GetBlockedBy(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations WHERE target_id = ? AND relation_type = 'blocks'`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

// Exists checks whether a specific (source, target, type) combination exists.
// Returns true if it does, false if it does not.
//
// Uses SQL's EXISTS subquery pattern which is more efficient than COUNT(*)
// because it stops as soon as it finds the first matching row.
func (r *RelationRepo) Exists(ctx context.Context, sourceID, targetID uuid.UUID, relType string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM relations WHERE source_id = ? AND target_id = ? AND relation_type = ?)`,
		sourceID.String(), targetID.String(), relType).Scan(&exists)
	return exists, err
}

// CountBlockingByTasks returns, for each task ID, how many other tasks it blocks.
// Tasks that block nothing are not included in the returned map.
func (r *RelationRepo) CountBlockingByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	return r.countRelationsByTasks(ctx, taskIDs, "source_id")
}

// CountBlockedByTasks returns, for each task ID, how many other tasks block it.
// Tasks that are not blocked are not included in the returned map.
func (r *RelationRepo) CountBlockedByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	return r.countRelationsByTasks(ctx, taskIDs, "target_id")
}

// CountBlockedByIncompleteTasks returns, for each task ID, how many incomplete
// tasks block it. A blocker is "incomplete" if its status is NOT 'completed' or
// 'deleted'. Tasks with zero incomplete blockers are absent from the map.
func (r *RelationRepo) CountBlockedByIncompleteTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	if len(taskIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}

	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		placeholders[i] = "?"
		args[i] = id.String()
	}

	query := fmt.Sprintf(
		`SELECT r.target_id, COUNT(*)
		 FROM relations r
		 JOIN tasks t ON r.source_id = t.id
		 WHERE r.target_id IN (%s)
		   AND r.relation_type = 'blocks'
		   AND t.status NOT IN ('completed', 'deleted')
		 GROUP BY r.target_id`,
		strings.Join(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var idStr string
		var count int
		if err := rows.Scan(&idStr, &count); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parsing id %q: %w", idStr, err)
		}
		counts[id] = count
	}
	return counts, rows.Err()
}

// countRelationsByTasks is a shared helper for counting blocking relations.
// column is either "source_id" (for blocking count) or "target_id" (for blocked-by count).
func (r *RelationRepo) countRelationsByTasks(ctx context.Context, taskIDs []uuid.UUID, column string) (map[uuid.UUID]int, error) {
	if len(taskIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}

	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		placeholders[i] = "?"
		args[i] = id.String()
	}

	query := fmt.Sprintf(
		`SELECT %s, COUNT(*) FROM relations WHERE %s IN (%s) AND relation_type = 'blocks' GROUP BY %s`,
		column, column, strings.Join(placeholders, ","), column,
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var idStr string
		var count int
		if err := rows.Scan(&idStr, &count); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parsing id %q: %w", idStr, err)
		}
		counts[id] = count
	}
	return counts, rows.Err()
}

type relationScanner interface {
	Scan(dest ...any) error
}

func scanRelation(s relationScanner) (*domain.Relation, error) {
	var (
		r                                 domain.Relation
		id, sourceID, targetID, createdAt string
	)
	if err := s.Scan(&id, &sourceID, &targetID, &r.RelationType, &createdAt); err != nil {
		return nil, err
	}
	var err error
	r.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	r.SourceID, err = uuid.Parse(sourceID)
	if err != nil {
		return nil, err
	}
	r.TargetID, err = uuid.Parse(targetID)
	if err != nil {
		return nil, err
	}
	r.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// scanRelations iterates over sql.Rows and assembles a slice of *domain.Relation.
// This function does NOT call rows.Close() — that is the caller's responsibility.
func scanRelations(rows *sql.Rows) ([]*domain.Relation, error) {
	result := make([]*domain.Relation, 0)
	for rows.Next() {
		rel, err := scanRelation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, rel)
	}
	return result, rows.Err()
}

// isUniqueViolation checks whether an error is a SQLite UNIQUE constraint violation.
//
// We check for this by looking at the error message string. This is pragmatic —
// Go's database/sql package does not define typed constraint errors, and the
// SQLite driver's error string has been stable for years.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
