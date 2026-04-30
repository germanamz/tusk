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

const projectColumns = `id, name, workflow_id, description, settings, version, created_at, updated_at`

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
func (repo *ProjectRepo) Create(ctx context.Context, project *domain.Project) error {
	settingsJSON, marshalErr := json.Marshal(project.Settings)

	if marshalErr != nil {
		return fmt.Errorf("marshaling settings: %w", marshalErr)
	}

	_, insertErr := repo.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO projects (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, projectColumns),
		project.ID.String(), project.Name, project.WorkflowID.String(), project.Description, string(settingsJSON), project.Version,
		project.CreatedAt.UTC().Format(timeFormat),
		project.UpdatedAt.UTC().Format(timeFormat),
	)

	if insertErr != nil {
		if _, lookupErr := repo.GetByName(ctx, project.Name); lookupErr == nil {
			return domain.ErrConflict
		}
		return insertErr
	}

	return nil
}

// GetByID retrieves a project by UUID. Returns domain.ErrNotFound if missing.
func (repo *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	row := repo.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects WHERE id = ?`, projectColumns),
		id.String())
	return scanProject(row)
}

// GetByName retrieves a project by name. Returns domain.ErrNotFound if missing.
func (repo *ProjectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	row := repo.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects WHERE name = ?`, projectColumns),
		name)
	return scanProject(row)
}

// List returns all projects ordered by name.
func (repo *ProjectRepo) List(ctx context.Context) ([]*domain.Project, error) {
	rows, err := repo.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects ORDER BY name`, projectColumns))

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	result := make([]*domain.Project, 0)
	for rows.Next() {
		project, scanErr := scanProject(rows)

		if scanErr != nil {
			return nil, scanErr
		}

		result = append(result, project)
	}
	return result, rows.Err()
}

type projectScanner interface {
	Scan(dest ...any) error
}

func scanProject(scanner projectScanner) (*domain.Project, error) {
	var (
		project      domain.Project
		idStr        string
		workflowStr  string
		settingsJSON string
		createdAt    string
		updatedAt    string
	)
	err := scanner.Scan(&idStr, &project.Name, &workflowStr, &project.Description, &settingsJSON, &project.Version, &createdAt, &updatedAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	project.ID, err = uuid.Parse(idStr)

	if err != nil {
		return nil, fmt.Errorf("parsing project id: %w", err)
	}

	project.WorkflowID, err = uuid.Parse(workflowStr)

	if err != nil {
		return nil, fmt.Errorf("parsing workflow_id: %w", err)
	}

	if err := json.Unmarshal([]byte(settingsJSON), &project.Settings); err != nil {
		return nil, fmt.Errorf("decoding settings: %w", err)
	}

	project.CreatedAt, err = time.Parse(timeFormat, createdAt)

	if err != nil {
		return nil, err
	}

	project.UpdatedAt, err = time.Parse(timeFormat, updatedAt)

	if err != nil {
		return nil, err
	}

	return &project, nil
}

// Update persists changes to a project with optimistic locking.
// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
func (repo *ProjectRepo) Update(ctx context.Context, project *domain.Project) error {
	settingsJSON, marshalErr := json.Marshal(project.Settings)

	if marshalErr != nil {
		return fmt.Errorf("marshaling settings: %w", marshalErr)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, execErr := repo.db.ExecContext(ctx, `
		UPDATE projects SET
			name = ?, workflow_id = ?, description = ?, settings = ?,
			version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		project.Name, project.WorkflowID.String(), project.Description, string(settingsJSON), nowStr,
		project.ID.String(), project.Version,
	)

	if execErr != nil {
		return execErr
	}

	rowCount, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowCount == 0 {
		var exists int
		lookupErr := repo.db.QueryRowContext(ctx,
			`SELECT 1 FROM projects WHERE id = ?`, project.ID.String()).Scan(&exists)

		if errors.Is(lookupErr, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	project.Version++
	project.UpdatedAt = now
	return nil
}

// CountProjectsByWorkflow returns how many projects reference the given workflow.
// Used by the workflow delete guard.
func (repo *ProjectRepo) CountProjectsByWorkflow(ctx context.Context, workflowID uuid.UUID) (int, error) {
	var count int
	err := repo.db.QueryRowContext(ctx,
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
func (repo *ProjectRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, execErr := repo.db.ExecContext(ctx,
		`DELETE FROM projects WHERE id = ? AND version = ?`,
		id.String(), version)

	if execErr != nil {
		return execErr
	}

	rowCount, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowCount == 0 {
		var exists int
		lookupErr := repo.db.QueryRowContext(ctx,
			`SELECT 1 FROM projects WHERE id = ?`, id.String()).Scan(&exists)

		if errors.Is(lookupErr, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	return nil
}
