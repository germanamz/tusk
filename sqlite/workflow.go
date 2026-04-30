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
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

var _ repository.WorkflowRepository = (*WorkflowRepo)(nil)

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
func (repo *WorkflowRepo) Create(ctx context.Context, workflow *domain.Workflow) error {
	statusesJSON, encodeStatusesErr := encodeStatuses(workflow.Statuses)

	if encodeStatusesErr != nil {
		return encodeStatusesErr
	}

	transitionsJSON, encodeTransitionsErr := encodeTransitions(workflow.Transitions)

	if encodeTransitionsErr != nil {
		return encodeTransitionsErr
	}

	_, execErr := repo.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO workflows (%s) VALUES (?, ?, ?, ?, ?, ?, ?)`, workflowColumns),
		workflow.ID.String(), workflow.Name, statusesJSON, transitionsJSON, workflow.Version,
		workflow.CreatedAt.UTC().Format(timeFormat),
		workflow.UpdatedAt.UTC().Format(timeFormat),
	)

	if execErr != nil {
		if _, lookupErr := repo.GetByName(ctx, workflow.Name); lookupErr == nil {
			return domain.ErrConflict
		}

		return execErr
	}

	return nil
}

// GetByID retrieves a workflow by UUID. Returns domain.ErrNotFound if missing.
func (repo *WorkflowRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error) {
	row := repo.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows WHERE id = ?`, workflowColumns),
		id.String())

	return scanWorkflow(row)
}

// GetByName retrieves a workflow by name. Returns domain.ErrNotFound if missing.
func (repo *WorkflowRepo) GetByName(ctx context.Context, name string) (*domain.Workflow, error) {
	row := repo.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows WHERE name = ?`, workflowColumns),
		name)

	return scanWorkflow(row)
}

// List returns all workflows ordered by name.
func (repo *WorkflowRepo) List(ctx context.Context) ([]*domain.Workflow, error) {
	rows, err := repo.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows ORDER BY name`, workflowColumns))

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	result := make([]*domain.Workflow, 0)

	for rows.Next() {
		workflow, scanErr := scanWorkflow(rows)

		if scanErr != nil {
			return nil, scanErr
		}

		result = append(result, workflow)
	}

	return result, rows.Err()
}

type workflowScanner interface {
	Scan(dest ...any) error
}

func scanWorkflow(scanner workflowScanner) (*domain.Workflow, error) {
	var (
		workflow        domain.Workflow
		idStr           string
		statusesJSON    string
		transitionsJSON string
		createdAt       string
		updatedAt       string
	)

	scanErr := scanner.Scan(&idStr, &workflow.Name, &statusesJSON, &transitionsJSON, &workflow.Version, &createdAt, &updatedAt)

	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}

		return nil, scanErr
	}

	parseIDErr := error(nil)
	workflow.ID, parseIDErr = uuid.Parse(idStr)

	if parseIDErr != nil {
		return nil, fmt.Errorf("parsing workflow id: %w", parseIDErr)
	}

	decodeStatusesErr := error(nil)
	workflow.Statuses, decodeStatusesErr = decodeStatuses([]byte(statusesJSON))

	if decodeStatusesErr != nil {
		return nil, fmt.Errorf("decoding statuses: %w", decodeStatusesErr)
	}

	decodeTransitionsErr := error(nil)
	workflow.Transitions, decodeTransitionsErr = decodeTransitions([]byte(transitionsJSON))

	if decodeTransitionsErr != nil {
		return nil, fmt.Errorf("decoding transitions: %w", decodeTransitionsErr)
	}

	parseCreatedAtErr := error(nil)
	workflow.CreatedAt, parseCreatedAtErr = time.Parse(timeFormat, createdAt)

	if parseCreatedAtErr != nil {
		return nil, parseCreatedAtErr
	}

	parseUpdatedAtErr := error(nil)
	workflow.UpdatedAt, parseUpdatedAtErr = time.Parse(timeFormat, updatedAt)

	if parseUpdatedAtErr != nil {
		return nil, parseUpdatedAtErr
	}

	return &workflow, nil
}

// Update persists changes to a workflow with optimistic locking.
// Returns domain.ErrConflict if the stored version has advanced,
// and domain.ErrNotFound if the row does not exist.
func (repo *WorkflowRepo) Update(ctx context.Context, workflow *domain.Workflow) error {
	statusesJSON, encodeStatusesErr := encodeStatuses(workflow.Statuses)

	if encodeStatusesErr != nil {
		return encodeStatusesErr
	}

	transitionsJSON, encodeTransitionsErr := encodeTransitions(workflow.Transitions)

	if encodeTransitionsErr != nil {
		return encodeTransitionsErr
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, execErr := repo.db.ExecContext(ctx, `
		UPDATE workflows SET
			name = ?, statuses = ?, transitions = ?,
			version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		workflow.Name, statusesJSON, transitionsJSON, nowStr,
		workflow.ID.String(), workflow.Version,
	)

	if execErr != nil {
		return execErr
	}

	rowsAffected, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowsAffected == 0 {
		var exists int

		existsErr := repo.db.QueryRowContext(ctx,
			`SELECT 1 FROM workflows WHERE id = ?`, workflow.ID.String()).Scan(&exists)

		if errors.Is(existsErr, sql.ErrNoRows) {
			return domain.ErrNotFound
		}

		return domain.ErrConflict
	}

	workflow.Version++
	workflow.UpdatedAt = now

	return nil
}

// Delete removes a workflow with optimistic locking on version.
// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
func (repo *WorkflowRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, execErr := repo.db.ExecContext(ctx,
		`DELETE FROM workflows WHERE id = ? AND version = ?`,
		id.String(), version)

	if execErr != nil {
		return execErr
	}

	rowsAffected, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowsAffected == 0 {
		var exists int

		existsErr := repo.db.QueryRowContext(ctx,
			`SELECT 1 FROM workflows WHERE id = ?`, id.String()).Scan(&exists)

		if errors.Is(existsErr, sql.ErrNoRows) {
			return domain.ErrNotFound
		}

		return domain.ErrConflict
	}

	return nil
}

func encodeStatuses(statusMap map[string]domain.StatusConfig) (string, error) {
	out := make(map[string][]domain.StatusRole, len(statusMap))

	for key, cfg := range statusMap {
		roles := cfg.Roles
		if roles == nil {
			roles = []domain.StatusRole{}
		}

		out[key] = roles
	}

	raw, marshalErr := json.Marshal(out)

	if marshalErr != nil {
		return "", marshalErr
	}

	return string(raw), nil
}

func decodeStatuses(raw []byte) (map[string]domain.StatusConfig, error) {
	var in map[string][]domain.StatusRole

	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}

	out := make(map[string]domain.StatusConfig, len(in))

	for key, roles := range in {
		out[key] = domain.StatusConfig{Roles: roles}
	}

	return out, nil
}

func encodeTransitions(transitions []domain.WorkflowTransition) (string, error) {
	out := make([][2]string, len(transitions))

	for index, transition := range transitions {
		out[index] = [2]string{transition.FromStatus, transition.ToStatus}
	}

	raw, marshalErr := json.Marshal(out)

	if marshalErr != nil {
		return "", marshalErr
	}

	return string(raw), nil
}

func decodeTransitions(raw []byte) ([]domain.WorkflowTransition, error) {
	var in [][2]string

	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}

	out := make([]domain.WorkflowTransition, len(in))

	for index, pair := range in {
		out[index] = domain.WorkflowTransition{FromStatus: pair[0], ToStatus: pair[1]}
	}

	return out, nil
}
