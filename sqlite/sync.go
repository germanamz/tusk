// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
)

// SyncConfigToDB ensures that every workflow and project defined in the
// TOML-loaded config exists in the SQLite workflows and projects tables.
// Existing rows are left untouched. This bridges the config-driven
// definitions with the FK-enforced SQLite schema so that task inserts can
// satisfy tasks.project_id → projects.id.
func SyncConfigToDB(
	ctx context.Context,
	cfg *config.Config,
	wfRepo *WorkflowRepo,
	projRepo *ProjectRepo,
) error {
	nowStr := time.Now().UTC().Truncate(time.Millisecond).Format(timeFormat)

	workflows := make(map[string]*domain.Workflow, len(cfg.Workflows))
	for name, wc := range cfg.Workflows {
		wf, err := config.WorkflowFromConfig(name, wc)
		if err != nil {
			return fmt.Errorf("building workflow %q: %w", name, err)
		}
		workflows[name] = wf
		existing, getErr := wfRepo.GetByID(ctx, wf.ID)
		if getErr == nil {
			statusesJSON, err := encodeStatuses(wf.Statuses)
			if err != nil {
				return fmt.Errorf("encoding workflow %q statuses: %w", name, err)
			}
			transitionsJSON, err := encodeTransitions(wf.Transitions)
			if err != nil {
				return fmt.Errorf("encoding workflow %q transitions: %w", name, err)
			}
			if _, err := wfRepo.db.ExecContext(ctx,
				`UPDATE workflows SET name = ?, statuses = ?, transitions = ?, updated_at = ? WHERE id = ?`,
				wf.Name, statusesJSON, transitionsJSON, nowStr, wf.ID.String(),
			); err != nil {
				return fmt.Errorf("syncing workflow %q: %w", name, err)
			}
			wf.Version = existing.Version
			continue
		}
		if !errors.Is(getErr, domain.ErrNotFound) {
			return fmt.Errorf("checking workflow %q: %w", name, getErr)
		}
		if err := wfRepo.Create(ctx, wf); err != nil && !errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("upserting workflow %q: %w", name, err)
		}
	}

	for name, pc := range cfg.Projects {
		p, err := config.ProjectFromConfig(name, pc, workflows)
		if err != nil {
			return fmt.Errorf("building project %q: %w", name, err)
		}
		_, getErr := projRepo.GetByID(ctx, p.ID)
		if getErr == nil {
			settingsJSON, err := json.Marshal(p.Settings)
			if err != nil {
				return fmt.Errorf("marshaling project %q settings: %w", name, err)
			}
			if _, err := projRepo.db.ExecContext(ctx,
				`UPDATE projects SET name = ?, workflow_id = ?, settings = ?, updated_at = ? WHERE id = ?`,
				p.Name, p.WorkflowID.String(), string(settingsJSON), nowStr, p.ID.String(),
			); err != nil {
				return fmt.Errorf("syncing project %q: %w", name, err)
			}
			continue
		}
		if !errors.Is(getErr, domain.ErrNotFound) {
			return fmt.Errorf("checking project %q: %w", name, getErr)
		}
		if err := projRepo.Create(ctx, p); err != nil && !errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("upserting project %q: %w", name, err)
		}
	}

	// Drop any SQL rows that no longer appear in the TOML config. Tasks still
	// write through config.*Project/*Workflow helpers in Phase 2, so the TOML
	// file remains authoritative for membership. Projects with referencing
	// tasks trip the FK and surface an error to the caller.
	projectIDs := make(map[string]struct{}, len(cfg.Projects))
	for name := range cfg.Projects {
		projectIDs[config.ProjectID(name).String()] = struct{}{}
	}
	existingProjects, err := projRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("listing existing projects: %w", err)
	}
	for _, p := range existingProjects {
		if _, ok := projectIDs[p.ID.String()]; ok {
			continue
		}
		if _, err := projRepo.db.ExecContext(ctx,
			`DELETE FROM projects WHERE id = ?`, p.ID.String(),
		); err != nil {
			return fmt.Errorf("dropping stale project %q: %w", p.Name, err)
		}
	}

	workflowIDs := make(map[string]struct{}, len(cfg.Workflows))
	for name := range cfg.Workflows {
		workflowIDs[config.WorkflowID(name).String()] = struct{}{}
	}
	existingWorkflows, err := wfRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("listing existing workflows: %w", err)
	}
	for _, wf := range existingWorkflows {
		if _, ok := workflowIDs[wf.ID.String()]; ok {
			continue
		}
		if _, err := wfRepo.db.ExecContext(ctx,
			`DELETE FROM workflows WHERE id = ?`, wf.ID.String(),
		); err != nil {
			return fmt.Errorf("dropping stale workflow %q: %w", wf.Name, err)
		}
	}
	return nil
}
