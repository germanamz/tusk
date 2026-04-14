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

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
)

// NewStore opens a fresh SQLite store inside t.TempDir, applies migrations,
// and seeds projects/workflows from cfg via sqlite.SyncConfigToDB. Returns
// the store plus project/workflow repos wired to it. Pass a nil cfg to
// skip seeding. Close is registered with t.Cleanup.
func NewStore(t testing.TB, cfg *config.Config) (*sqlite.Store, *sqlite.ProjectRepo, *sqlite.WorkflowRepo) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "test.db"), migrations.FS)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	projRepo := sqlite.NewProjectRepo(store.DB())
	wfRepo := sqlite.NewWorkflowRepo(store.DB())
	if cfg != nil {
		if err := sqlite.SyncConfigToDB(context.Background(), cfg, wfRepo, projRepo); err != nil {
			t.Fatalf("sqlite.SyncConfigToDB: %v", err)
		}
	}
	return store, projRepo, wfRepo
}

// KanbanConfig builds a minimal *config.Config containing a kanban
// workflow (pending/active/completed/deleted with the usual transitions)
// and the given project names, all bound to kanban. Callers that need
// additional workflows or custom settings should build the config
// manually.
func KanbanConfig(projects ...string) *config.Config {
	cfg := &config.Config{
		Workflows: map[string]config.WorkflowConfig{
			"kanban": KanbanWorkflow(),
		},
		Projects: map[string]config.ProjectConfig{},
	}
	for _, name := range projects {
		cfg.Projects[name] = config.ProjectConfig{Workflow: "kanban"}
	}
	return cfg
}

// KanbanWorkflow returns the canonical kanban workflow config used by
// most tests: pending → active ⇄ pending, active → completed, both
// pending and active → deleted, completed → pending.
func KanbanWorkflow() config.WorkflowConfig {
	return config.WorkflowConfig{
		Statuses: map[string]config.StatusConfig{
			"pending":   {Roles: []string{config.RoleInitial}},
			"active":    {Roles: []string{config.RoleStart, config.RoleHighlight}},
			"completed": {Roles: []string{config.RoleTerminal, config.RoleDone, config.RoleDim}},
			"deleted":   {Roles: []string{config.RoleTerminal, config.RoleDelete, config.RoleDim}},
		},
		Transitions: []config.WorkflowTransitionConfig{
			{From: "pending", To: "active"},
			{From: "pending", To: "deleted"},
			{From: "active", To: "completed"},
			{From: "active", To: "pending"},
			{From: "active", To: "deleted"},
			{From: "completed", To: "pending"},
		},
	}
}
