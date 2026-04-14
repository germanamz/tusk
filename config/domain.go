// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// WorkflowFromConfig constructs a domain.Workflow from a named WorkflowConfig.
// The ID is derived deterministically from the workflow name via UUIDv5
// (uuid.Nil namespace, "workflow:<name>"), except "kanban" which maps to
// uuid.Nil to match the built-in default project's workflow.
func WorkflowFromConfig(name string, cfg WorkflowConfig) (*domain.Workflow, error) {
	id := WorkflowID(name)
	now := time.Now().UTC().Truncate(time.Millisecond)
	wf := &domain.Workflow{
		ID:          id,
		Name:        name,
		Statuses:    make(map[string]domain.StatusConfig, len(cfg.Statuses)),
		Transitions: make([]domain.WorkflowTransition, len(cfg.Transitions)),
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	for statusName, sc := range cfg.Statuses {
		roles := make([]domain.StatusRole, len(sc.Roles))
		for i, r := range sc.Roles {
			roles[i] = domain.StatusRole(r)
		}
		wf.Statuses[statusName] = domain.StatusConfig{Roles: roles}
	}
	for i, t := range cfg.Transitions {
		wf.Transitions[i] = domain.WorkflowTransition{
			FromStatus: t.From,
			ToStatus:   t.To,
		}
	}
	return wf, nil
}

// ProjectFromConfig constructs a domain.Project from a named ProjectConfig.
// The workflows map (keyed by name) is used to resolve the referenced
// workflow ID; the map should typically be the result of calling
// WorkflowFromConfig for every entry in Config.Workflows.
func ProjectFromConfig(name string, cfg ProjectConfig, workflows map[string]*domain.Workflow) (*domain.Project, error) {
	id := ProjectID(name)
	wf, ok := workflows[cfg.Workflow]
	if !ok {
		return nil, fmt.Errorf("project %q references unknown workflow %q", name, cfg.Workflow)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	p := &domain.Project{
		ID:         id,
		Name:       name,
		WorkflowID: wf.ID,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if cfg.Settings.AutoCompleteParent != nil {
		p.Settings.AutoCompleteParent = &domain.AutoCompleteConfig{
			TriggerStatus: cfg.Settings.AutoCompleteParent.TriggerStatus,
			TargetStatus:  cfg.Settings.AutoCompleteParent.TargetStatus,
		}
	}
	if cfg.Settings.AutoRevertParent != nil {
		p.Settings.AutoRevertParent = &domain.AutoRevertConfig{
			TriggerStatus: cfg.Settings.AutoRevertParent.TriggerStatus,
			TargetStatus:  cfg.Settings.AutoRevertParent.TargetStatus,
		}
	}
	if cfg.Settings.Urgency != nil {
		p.Settings.Urgency = &domain.UrgencyOverrides{
			PriorityWeight:    cfg.Settings.Urgency.PriorityWeight,
			DueWeight:         cfg.Settings.Urgency.DueWeight,
			AgeWeight:         cfg.Settings.Urgency.AgeWeight,
			ActiveWeight:      cfg.Settings.Urgency.ActiveWeight,
			BlockingWeight:    cfg.Settings.Urgency.BlockingWeight,
			BlockedWeight:     cfg.Settings.Urgency.BlockedWeight,
			TagsWeight:        cfg.Settings.Urgency.TagsWeight,
			ProjectWeight:     cfg.Settings.Urgency.ProjectWeight,
			AnnotationsWeight: cfg.Settings.Urgency.AnnotationsWeight,
			WaitingWeight:     cfg.Settings.Urgency.WaitingWeight,
		}
	}
	return p, nil
}

// WorkflowID derives the deterministic UUID for a workflow by name.
func WorkflowID(name string) uuid.UUID {
	if name == "kanban" {
		return uuid.Nil
	}
	return uuid.NewSHA1(uuid.Nil, []byte("workflow:"+name))
}

// ProjectID derives the deterministic UUID for a project by name.
func ProjectID(name string) uuid.UUID {
	if name == DefaultProjectID {
		return uuid.Nil
	}
	return uuid.NewSHA1(uuid.Nil, []byte("project:"+name))
}
