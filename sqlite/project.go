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

var _ repository.ProjectRepository = (*ProjectRepo)(nil)

const projectColumns = `id, name, workflow_id, settings, version, created_at, updated_at`

// ProjectRepo implements project persistence using SQLite.
type ProjectRepo struct {
	db DBTX
}

// NewProjectRepo creates a ProjectRepo.
func NewProjectRepo(db DBTX) *ProjectRepo {
	return &ProjectRepo{db: db}
}

// Create inserts a new project. Returns domain.ErrConflict on unique-name collision.
// FK violations on workflow_id surface as the raw SQLite error.
func (r *ProjectRepo) Create(ctx context.Context, p *domain.Project) error {
	settingsJSON, err := json.Marshal(p.Settings)
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO projects (%s) VALUES (?, ?, ?, ?, ?, ?, ?)`, projectColumns),
		p.ID.String(), p.Name, p.WorkflowID.String(), string(settingsJSON), p.Version,
		p.CreatedAt.UTC().Format(timeFormat),
		p.UpdatedAt.UTC().Format(timeFormat),
	)
	if err != nil {
		if _, lookupErr := r.GetByName(ctx, p.Name); lookupErr == nil {
			return domain.ErrConflict
		}
		return err
	}
	return nil
}

// GetByID retrieves a project by UUID. Returns domain.ErrNotFound if missing.
func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects WHERE id = ?`, projectColumns),
		id.String())
	return scanProject(row)
}

// GetByName retrieves a project by name. Returns domain.ErrNotFound if missing.
func (r *ProjectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects WHERE name = ?`, projectColumns),
		name)
	return scanProject(row)
}

// List returns all projects ordered by name.
func (r *ProjectRepo) List(ctx context.Context) ([]*domain.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects ORDER BY name`, projectColumns))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

type projectScanner interface {
	Scan(dest ...any) error
}

func scanProject(s projectScanner) (*domain.Project, error) {
	var (
		p            domain.Project
		idStr        string
		workflowStr  string
		settingsJSON string
		createdAt    string
		updatedAt    string
	)
	err := s.Scan(&idStr, &p.Name, &workflowStr, &settingsJSON, &p.Version, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	p.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parsing project id: %w", err)
	}
	p.WorkflowID, err = uuid.Parse(workflowStr)
	if err != nil {
		return nil, fmt.Errorf("parsing workflow_id: %w", err)
	}
	if err := json.Unmarshal([]byte(settingsJSON), &p.Settings); err != nil {
		return nil, fmt.Errorf("decoding settings: %w", err)
	}
	p.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt, err = time.Parse(timeFormat, updatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Update persists changes to a project with optimistic locking.
// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
func (r *ProjectRepo) Update(ctx context.Context, p *domain.Project) error {
	settingsJSON, err := json.Marshal(p.Settings)
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `
		UPDATE projects SET
			name = ?, workflow_id = ?, settings = ?,
			version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		p.Name, p.WorkflowID.String(), string(settingsJSON), nowStr,
		p.ID.String(), p.Version,
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
			`SELECT 1 FROM projects WHERE id = ?`, p.ID.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	p.Version++
	p.UpdatedAt = now
	return nil
}

// CountProjectsByWorkflow returns how many projects reference the given workflow.
// Used by the workflow delete guard.
func (r *ProjectRepo) CountProjectsByWorkflow(ctx context.Context, workflowID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM projects WHERE workflow_id = ?`,
		workflowID.String()).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Delete removes a project with optimistic locking on version.
// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
// After Phase 3, this call may also surface a SQLite FK error when the project
// is still referenced by tasks — that is expected and handled by the service layer.
func (r *ProjectRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM projects WHERE id = ? AND version = ?`,
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
			`SELECT 1 FROM projects WHERE id = ?`, id.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	return nil
}
