// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

func TestMigration005_TasksHaveFKToProjects(t *testing.T) {
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer store.Close()

	tr := sqlite.NewTaskRepo(store.DB())
	now := time.Now().UTC().Truncate(time.Millisecond)

	ok := &domain.Task{
		ID:         uuid.New(),
		ShortID:    "abcd1234",
		Title:      "fk-ok",
		ProjectID:  domain.DefaultProjectUUID,
		Status:     "pending",
		Version:    1,
		UDA:        map[string]any{},
		CreatedAt:  now,
		ModifiedAt: now,
	}
	if err := tr.Create(context.Background(), ok); err != nil {
		t.Fatalf("insert with seeded project: %v", err)
	}

	bad := &domain.Task{
		ID:         uuid.New(),
		ShortID:    "abcd5678",
		Title:      "fk-bad",
		ProjectID:  uuid.New(),
		Status:     "pending",
		Version:    1,
		UDA:        map[string]any{},
		CreatedAt:  now,
		ModifiedAt: now,
	}
	err = tr.Create(context.Background(), bad)
	if err == nil {
		t.Fatalf("expected FK violation, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("expected foreign-key error, got: %v", err)
	}
}
