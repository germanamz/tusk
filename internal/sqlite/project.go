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

// ProjectRepo implements repository.ProjectRepository using SQLite.
//
// It stores *sql.DB (not *Store) so it depends only on the standard library's
// database abstraction. The Store is responsible for opening the DB and running
// migrations; ProjectRepo just runs queries.
type ProjectRepo struct {
	db DBTX
}

// NewProjectRepo creates a ProjectRepo. Pass in the *sql.DB from Store.DB().
//
// Example:
//
//	store, _ := sqlite.New("tusk.db", migrations.FS)
//	repo := sqlite.NewProjectRepo(store.DB())
func NewProjectRepo(db DBTX) *ProjectRepo {
	return &ProjectRepo{db: db}
}

// Create inserts a new project into the database.
//
// The caller must set all fields on the Project struct before calling Create.
// The ID should be generated with uuid.New() and CreatedAt should be set to
// time.Now().UTC().
//
// If a project with the same name already exists, SQLite returns a UNIQUE
// constraint error (because the name column has a UNIQUE constraint).
func (r *ProjectRepo) Create(ctx context.Context, project *domain.Project) error {
	settingsJSON, err := json.Marshal(project.Settings)
	if err != nil {
		return fmt.Errorf("marshaling project settings: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, description, default_workflow, settings, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		project.ID.String(), project.Name, project.Description,
		project.DefaultWorkflow, string(settingsJSON),
		project.CreatedAt.UTC().Format(timeFormat),
	)
	return err
}

// GetByID retrieves a project by its UUID primary key.
// Returns domain.ErrNotFound if no project has that ID.
func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return r.scanOne(r.db.QueryRowContext(ctx,
		`SELECT id, name, description, default_workflow, settings, created_at
		 FROM projects WHERE id = ?`, id.String()))
}

// GetByName retrieves a project by its unique name.
// Returns domain.ErrNotFound if no project has that name.
func (r *ProjectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return r.scanOne(r.db.QueryRowContext(ctx,
		`SELECT id, name, description, default_workflow, settings, created_at
		 FROM projects WHERE name = ?`, name))
}

// List returns all projects in the database.
// The result includes the seeded "_default" project.
// Returns an empty slice (not nil) if somehow there are no projects,
// though in practice the seeded project is always present.
func (r *ProjectRepo) List(ctx context.Context) ([]*domain.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, default_workflow, settings, created_at FROM projects`)
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

// Update modifies an existing project's name, description, and default_workflow.
// The project is identified by its ID. CreatedAt is NOT updated (it is immutable).
//
// Note: this method does NOT check RowsAffected. If the ID does not exist,
// the UPDATE silently affects 0 rows. This is intentional — Update is typically
// called right after GetByID, so the project is known to exist.
func (r *ProjectRepo) Update(ctx context.Context, project *domain.Project) error {
	settingsJSON, err := json.Marshal(project.Settings)
	if err != nil {
		return fmt.Errorf("marshaling project settings: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, default_workflow = ?, settings = ?
		 WHERE id = ?`,
		project.Name, project.Description, project.DefaultWorkflow,
		string(settingsJSON), project.ID.String(),
	)
	return err
}

// Delete removes a project by ID.
// Returns domain.ErrNotFound if no project with that ID exists.
//
// Unlike Update, Delete checks RowsAffected because deleting a non-existent
// resource is an error the caller should know about (e.g., to return 404).
func (r *ProjectRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id.String())
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

// scanOne is a helper that scans a single row and translates sql.ErrNoRows
// into domain.ErrNotFound. It is used by GetByID and GetByName.
//
// Why a separate method instead of inlining this in GetByID/GetByName?
// Because the translation logic (sql.ErrNoRows -> domain.ErrNotFound) is
// easy to forget or get wrong. Having it in one place means we only need
// to get it right once.
func (r *ProjectRepo) scanOne(row *sql.Row) (*domain.Project, error) {
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return p, err
}

// projectScanner is a tiny interface satisfied by both *sql.Row and *sql.Rows.
// This lets scanProject work with either type, avoiding code duplication.
//
// *sql.Row is returned by QueryRowContext (single row lookup).
// *sql.Rows is returned by QueryContext (multiple row iteration).
// Both have a Scan method with the same signature.
type projectScanner interface {
	Scan(dest ...any) error
}

// scanProject reads one row of project data from a scanner.
// It is a package-level function (not a method on ProjectRepo) because it
// does not need access to the database — it only needs the scanner.
//
// The function:
//  1. Scans the 6 columns (id, name, description, default_workflow, settings, created_at)
//     into local variables. id, settings, and created_at are scanned as strings
//     because SQLite stores them as TEXT.
//  2. Parses the id string into a uuid.UUID.
//  3. Unmarshals the settings JSON string into domain.ProjectSettings.
//  4. Parses the created_at string into a time.Time using timeFormat.
//  5. Returns the assembled *domain.Project.
func scanProject(s projectScanner) (*domain.Project, error) {
	var (
		p            domain.Project
		id           string
		settingsJSON string
		createdAt    string
	)
	err := s.Scan(&id, &p.Name, &p.Description, &p.DefaultWorkflow, &settingsJSON, &createdAt)
	if err != nil {
		return nil, err
	}
	p.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(settingsJSON), &p.Settings); err != nil {
		return nil, fmt.Errorf("parsing project settings: %w", err)
	}
	p.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
