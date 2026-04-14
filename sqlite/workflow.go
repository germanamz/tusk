// Package sqlite: workflow repository implementation.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

const workflowColumns = `id, name, statuses, transitions, version, created_at, updated_at`

// WorkflowRepo implements repository.WorkflowRepository using SQLite.
type WorkflowRepo struct {
	db DBTX
}

// NewWorkflowRepo creates a WorkflowRepo.
func NewWorkflowRepo(db DBTX) *WorkflowRepo {
	return &WorkflowRepo{db: db}
}

// Create inserts a new workflow. Returns domain.ErrConflict on unique-name collision.
func (r *WorkflowRepo) Create(ctx context.Context, wf *domain.Workflow) error {
	statusesJSON, err := encodeStatuses(wf.Statuses)
	if err != nil {
		return err
	}
	transitionsJSON, err := encodeTransitions(wf.Transitions)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO workflows (%s) VALUES (?, ?, ?, ?, ?, ?, ?)`, workflowColumns),
		wf.ID.String(), wf.Name, statusesJSON, transitionsJSON, wf.Version,
		wf.CreatedAt.UTC().Format(timeFormat),
		wf.UpdatedAt.UTC().Format(timeFormat),
	)
	if err != nil {
		if _, lookupErr := r.GetByName(ctx, wf.Name); lookupErr == nil {
			return domain.ErrConflict
		}
		return err
	}
	return nil
}

// GetByID retrieves a workflow by UUID. Returns domain.ErrNotFound if missing.
func (r *WorkflowRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows WHERE id = ?`, workflowColumns),
		id.String())
	return scanWorkflow(row)
}

// GetByName retrieves a workflow by name. Returns domain.ErrNotFound if missing.
func (r *WorkflowRepo) GetByName(ctx context.Context, name string) (*domain.Workflow, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows WHERE name = ?`, workflowColumns),
		name)
	return scanWorkflow(row)
}

// List returns all workflows ordered by name.
func (r *WorkflowRepo) List(ctx context.Context) ([]*domain.Workflow, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows ORDER BY name`, workflowColumns))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Workflow, 0)
	for rows.Next() {
		wf, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, wf)
	}
	return result, rows.Err()
}

type workflowScanner interface {
	Scan(dest ...any) error
}

func scanWorkflow(s workflowScanner) (*domain.Workflow, error) {
	var (
		wf              domain.Workflow
		idStr           string
		statusesJSON    string
		transitionsJSON string
		createdAt       string
		updatedAt       string
	)
	err := s.Scan(&idStr, &wf.Name, &statusesJSON, &transitionsJSON, &wf.Version, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	wf.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parsing workflow id: %w", err)
	}
	wf.Statuses, err = decodeStatuses([]byte(statusesJSON))
	if err != nil {
		return nil, fmt.Errorf("decoding statuses: %w", err)
	}
	wf.Transitions, err = decodeTransitions([]byte(transitionsJSON))
	if err != nil {
		return nil, fmt.Errorf("decoding transitions: %w", err)
	}
	wf.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	wf.UpdatedAt, err = time.Parse(timeFormat, updatedAt)
	if err != nil {
		return nil, err
	}
	return &wf, nil
}

// Update persists changes to a workflow with optimistic locking.
// Returns domain.ErrConflict if the stored version has advanced,
// and domain.ErrNotFound if the row does not exist.
func (r *WorkflowRepo) Update(ctx context.Context, wf *domain.Workflow) error {
	statusesJSON, err := encodeStatuses(wf.Statuses)
	if err != nil {
		return err
	}
	transitionsJSON, err := encodeTransitions(wf.Transitions)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `
		UPDATE workflows SET
			name = ?, statuses = ?, transitions = ?,
			version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		wf.Name, statusesJSON, transitionsJSON, nowStr,
		wf.ID.String(), wf.Version,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		err := r.db.QueryRowContext(ctx,
			`SELECT 1 FROM workflows WHERE id = ?`, wf.ID.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	wf.Version++
	wf.UpdatedAt = now
	return nil
}

// Delete removes a workflow with optimistic locking on version.
// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
func (r *WorkflowRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM workflows WHERE id = ? AND version = ?`,
		id.String(), version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		err := r.db.QueryRowContext(ctx,
			`SELECT 1 FROM workflows WHERE id = ?`, id.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	return nil
}

func encodeStatuses(m map[string]domain.StatusConfig) (string, error) {
	out := make(map[string][]domain.StatusRole, len(m))
	for k, v := range m {
		roles := v.Roles
		if roles == nil {
			roles = []domain.StatusRole{}
		}
		out[k] = roles
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeStatuses(b []byte) (map[string]domain.StatusConfig, error) {
	var in map[string][]domain.StatusRole
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	out := make(map[string]domain.StatusConfig, len(in))
	for k, v := range in {
		out[k] = domain.StatusConfig{Roles: v}
	}
	return out, nil
}

func encodeTransitions(ts []domain.WorkflowTransition) (string, error) {
	out := make([][2]string, len(ts))
	for i, t := range ts {
		out[i] = [2]string{t.FromStatus, t.ToStatus}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeTransitions(b []byte) ([]domain.WorkflowTransition, error) {
	var in [][2]string
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	out := make([]domain.WorkflowTransition, len(in))
	for i, pair := range in {
		out[i] = domain.WorkflowTransition{FromStatus: pair[0], ToStatus: pair[1]}
	}
	return out, nil
}
