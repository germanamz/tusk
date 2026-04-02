package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type WorkflowRepo struct {
	db *sql.DB
}

func NewWorkflowRepo(db *sql.DB) *WorkflowRepo {
	return &WorkflowRepo{db: db}
}

func (r *WorkflowRepo) GetByProjectAndName(ctx context.Context, projectID uuid.UUID, name string) (*domain.Workflow, error) {
	var (wf domain.Workflow; id, pid, statusesJSON string)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, statuses FROM workflows WHERE project_id = ? AND name = ?`,
		projectID.String(), name,
	).Scan(&id, &pid, &wf.Name, &statusesJSON)
	if errors.Is(err, sql.ErrNoRows) { return nil, domain.ErrNotFound }
	if err != nil { return nil, err }
	wf.ID, err = uuid.Parse(id)
	if err != nil { return nil, err }
	wf.ProjectID, err = uuid.Parse(pid)
	if err != nil { return nil, err }
	if err := json.Unmarshal([]byte(statusesJSON), &wf.Statuses); err != nil { return nil, err }
	return &wf, nil
}

func (r *WorkflowRepo) GetTransitions(ctx context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workflow_id, from_status, to_status FROM workflow_transitions WHERE workflow_id = ?`,
		workflowID.String())
	if err != nil { return nil, err }
	defer rows.Close()
	var result []*domain.WorkflowTransition
	for rows.Next() {
		var (t domain.WorkflowTransition; id, wid string)
		if err := rows.Scan(&id, &wid, &t.FromStatus, &t.ToStatus); err != nil { return nil, err }
		t.ID, err = uuid.Parse(id)
		if err != nil { return nil, err }
		t.WorkflowID, err = uuid.Parse(wid)
		if err != nil { return nil, err }
		result = append(result, &t)
	}
	return result, rows.Err()
}

func (r *WorkflowRepo) Create(ctx context.Context, wf *domain.Workflow) error {
	statusesJSON, err := json.Marshal(wf.Statuses)
	if err != nil { return err }
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO workflows (id, project_id, name, statuses) VALUES (?, ?, ?, ?)`,
		wf.ID.String(), wf.ProjectID.String(), wf.Name, string(statusesJSON))
	return err
}

func (r *WorkflowRepo) AddTransition(ctx context.Context, t *domain.WorkflowTransition) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_transitions (id, workflow_id, from_status, to_status) VALUES (?, ?, ?, ?)`,
		t.ID.String(), t.WorkflowID.String(), t.FromStatus, t.ToStatus)
	return err
}
