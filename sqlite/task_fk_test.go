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

func TestMigration005_TasksHaveFKToProjects(test *testing.T) {
	store, err := sqlite.New(test.TempDir()+"/test.db", migrations.FS)

	if err != nil {
		test.Fatalf("opening test db: %v", err)
	}

	defer store.Close()

	taskRepo := sqlite.NewTaskRepo(store.DB())
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

	if err := taskRepo.Create(context.Background(), ok); err != nil {
		test.Fatalf("insert with seeded project: %v", err)
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
	err = taskRepo.Create(context.Background(), bad)
	if err == nil {
		test.Fatalf("expected FK violation, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		test.Errorf("expected foreign-key error, got: %v", err)
	}
}
