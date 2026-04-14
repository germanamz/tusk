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
// Both sides are now seed-only: existing rows are authoritative and left
// alone (ProjectService and WorkflowService own writes). Missing rows are
// inserted on first run; subsequent startups leave DB-only rows untouched.
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
		_, getErr := wfRepo.GetByID(ctx, wf.ID)
		if getErr == nil {
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
		existing, getErr := projRepo.GetByID(ctx, p.ID)
		if getErr == nil {
			// Row already present — DB is authoritative. ProjectService owns
			// subsequent writes; TOML is only a seed on first run.
			//
			// Exception: the built-in default project is pre-inserted by
			// migration 004 in a pristine state (version == 1, empty
			// settings, workflow_id = kanban). If the user has not yet
			// modified it via `tusk project modify`, apply the TOML-derived
			// workflow binding and settings as a one-time bootstrap so users
			// who configure a custom workflow or automation in TOML still
			// see them take effect. Once they run `tusk project modify`,
			// version becomes ≥ 2 and this branch is skipped forever — the
			// DB stays authoritative as planned.
			if p.ID == domain.DefaultProjectUUID && existing.Version == 1 && isZeroProjectSettings(existing.Settings) {
				settingsJSON, err := json.Marshal(p.Settings)
				if err != nil {
					return fmt.Errorf("marshaling project %q settings: %w", name, err)
				}
				if _, err := projRepo.db.ExecContext(ctx,
					`UPDATE projects SET workflow_id = ?, settings = ?, updated_at = ? WHERE id = ? AND version = 1`,
					p.WorkflowID.String(), string(settingsJSON), nowStr, p.ID.String(),
				); err != nil {
					return fmt.Errorf("bootstrapping default project: %w", err)
				}
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

	return nil
}

// isZeroProjectSettings reports whether the given settings value has no
// configured automation or urgency overrides — i.e. whether it matches the
// empty state that migration 004 seeds for the default project.
func isZeroProjectSettings(s domain.ProjectSettings) bool {
	return s.AutoCompleteParent == nil && s.AutoRevertParent == nil && s.Urgency == nil
}
