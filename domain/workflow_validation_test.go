// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"testing"
)

// baseValidWorkflow builds a workflow that satisfies every rule. Tests mutate
// a copy to assert specific failures.
func baseValidWorkflow() *Workflow {
	return &Workflow{
		Name: "sprint",
		Statuses: map[string]StatusConfig{
			"pending":   {Roles: []StatusRole{RoleInitial}},
			"active":    {Roles: []StatusRole{RoleStart, RoleHighlight}},
			"completed": {Roles: []StatusRole{RoleTerminal, RoleDone, RoleDim}},
			"deleted":   {Roles: []StatusRole{RoleTerminal, RoleDelete, RoleDim}},
		},
		Transitions: []WorkflowTransition{
			{FromStatus: "pending", ToStatus: "active"},
			{FromStatus: "active", ToStatus: "completed"},
			{FromStatus: "active", ToStatus: "deleted"},
		},
	}
}

func TestValidateWorkflow_Happy(t *testing.T) {
	if err := ValidateWorkflow(baseValidWorkflow()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateWorkflow_NilWorkflow(t *testing.T) {
	err := ValidateWorkflow(nil)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_NoStatuses(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Statuses = nil
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_UnknownRole(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Statuses["pending"] = StatusConfig{Roles: []StatusRole{"bogus", RoleInitial}}
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_MissingInitial(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Statuses["pending"] = StatusConfig{}
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_DuplicateStart(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Statuses["pending"] = StatusConfig{Roles: []StatusRole{RoleInitial, RoleStart}}
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_MissingTerminal(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Statuses: map[string]StatusConfig{
			"a": {Roles: []StatusRole{RoleInitial}},
			"b": {Roles: []StatusRole{RoleStart}},
			"c": {Roles: []StatusRole{RoleDone}},
			"d": {Roles: []StatusRole{RoleDelete}},
		},
		Transitions: []WorkflowTransition{{FromStatus: "a", ToStatus: "b"}},
	}
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_DoneWithoutTerminal(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Statuses["completed"] = StatusConfig{Roles: []StatusRole{RoleDone}}
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_DeleteWithoutTerminal(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Statuses["deleted"] = StatusConfig{Roles: []StatusRole{RoleDelete}}
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_HighlightAndDim(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Statuses["active"] = StatusConfig{Roles: []StatusRole{RoleStart, RoleHighlight, RoleDim}}
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_OrphanTransition(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Transitions = append(wf.Transitions, WorkflowTransition{FromStatus: "active", ToStatus: "ghost"})
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_MissingInitialToStartTransition(t *testing.T) {
	wf := baseValidWorkflow()
	wf.Transitions = []WorkflowTransition{
		{FromStatus: "active", ToStatus: "completed"},
		{FromStatus: "active", ToStatus: "deleted"},
	}
	err := ValidateWorkflow(wf)
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}
