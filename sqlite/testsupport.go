// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/migrations"
)

// NewTestStore opens a fresh file-backed SQLite store under t.TempDir()
// with all migrations applied. The store is closed via t.Cleanup.
//
// Migration 003 seeds the built-in "kanban" workflow and migration 004
// seeds the built-in "default" project, so tests that rely only on
// those defaults do not need to sync a config.
func NewTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := New(filepath.Join(dir, "test.db"), migrations.FS)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// NewTestConfigRepos constructs ProjectRepo and WorkflowRepo against
// store and optionally seeds them from cfg via SyncConfigToDB. Pass
// nil cfg when the default seeds provided by migrations 003/004 are
// sufficient.
func NewTestConfigRepos(t *testing.T, store *Store, cfg *config.Config) (*ProjectRepo, *WorkflowRepo) {
	t.Helper()
	projRepo := NewProjectRepo(store.DB())
	wfRepo := NewWorkflowRepo(store.DB())
	if cfg != nil {
		if err := SyncConfigToDB(context.Background(), cfg, wfRepo, projRepo); err != nil {
			t.Fatalf("SyncConfigToDB: %v", err)
		}
	}
	return projRepo, wfRepo
}
