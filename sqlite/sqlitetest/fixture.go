// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

// Package sqlitetest provides SQLite-backed fixtures shared across test
// suites. Kept out of the main sqlite package so the production binary
// does not import the standard library testing package.
package sqlitetest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// NewStore opens a fresh SQLite store inside harness.TempDir, applies migrations,
// and returns the store plus project/workflow repos wired to it. Migrations
// alone seed the builtin kanban workflow and the default project row — no
// additional seeding is required. Close is registered with harness.Cleanup.
func NewStore(harness testing.TB) (*sqlite.Store, *sqlite.ProjectRepo, *sqlite.WorkflowRepo) {
	harness.Helper()
	dir := harness.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "test.db"), migrations.FS)

	if err != nil {
		harness.Fatalf("sqlite.New: %v", err)
	}

	harness.Cleanup(func() { _ = store.Close() })

	projRepo := sqlite.NewProjectRepo(store.DB())
	wfRepo := sqlite.NewWorkflowRepo(store.DB())
	return store, projRepo, wfRepo
}

// SeedProject inserts a project with the given name bound to the builtin
// kanban workflow (uuid.Nil). Tests that need an extra project beyond the
// migration-seeded default call this to avoid hand-rolling repo writes.
func SeedProject(harness testing.TB, repo *sqlite.ProjectRepo, name string) *domain.Project {
	harness.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	project := &domain.Project{
		ID:         uuid.New(),
		Name:       name,
		WorkflowID: uuid.Nil,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := repo.Create(context.Background(), project); err != nil {
		harness.Fatalf("seed project %q: %v", name, err)
	}
	return project
}
