// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
)

// SyncConfigToDB ensures that every workflow and project known to the
// config-backed repositories exists in the SQLite workflows and projects
// tables. Existing rows are left untouched. This bridges the config-driven
// inmem repositories with the FK-enforced SQLite schema so that task inserts
// can satisfy tasks.project_id → projects.id.
func SyncConfigToDB(
	ctx context.Context,
	workflows repository.WorkflowRepository,
	projects repository.ProjectRepository,
	wfRepo *WorkflowRepo,
	projRepo *ProjectRepo,
) error {
	wfList, err := workflows.List(ctx)
	if err != nil {
		return fmt.Errorf("listing config workflows: %w", err)
	}
	for _, wf := range wfList {
		_, getErr := wfRepo.GetByID(ctx, wf.ID)
		if getErr == nil {
			continue
		}
		if !errors.Is(getErr, domain.ErrNotFound) {
			return fmt.Errorf("checking workflow %q: %w", wf.Name, getErr)
		}
		if err := wfRepo.Create(ctx, wf); err != nil && !errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("upserting workflow %q: %w", wf.Name, err)
		}
	}

	projList, err := projects.List(ctx)
	if err != nil {
		return fmt.Errorf("listing config projects: %w", err)
	}
	for _, p := range projList {
		_, getErr := projRepo.GetByID(ctx, p.ID)
		if getErr == nil {
			continue
		}
		if !errors.Is(getErr, domain.ErrNotFound) {
			return fmt.Errorf("checking project %q: %w", p.Name, getErr)
		}
		if err := projRepo.Create(ctx, p); err != nil && !errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("upserting project %q: %w", p.Name, err)
		}
	}
	return nil
}
