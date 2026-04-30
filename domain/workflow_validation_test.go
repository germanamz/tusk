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

func TestValidateWorkflow_Happy(test *testing.T) {
	if err := ValidateWorkflow(baseValidWorkflow()); err != nil {
		test.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateWorkflow_NilWorkflow(test *testing.T) {
	err := ValidateWorkflow(nil)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_NoStatuses(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Statuses = nil
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_UnknownRole(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Statuses["pending"] = StatusConfig{Roles: []StatusRole{"bogus", RoleInitial}}
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_MissingInitial(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Statuses["pending"] = StatusConfig{}
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_DuplicateStart(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Statuses["pending"] = StatusConfig{Roles: []StatusRole{RoleInitial, RoleStart}}
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_MissingTerminal(test *testing.T) {
	workflow := &Workflow{
		Name: "w",
		Statuses: map[string]StatusConfig{
			"a": {Roles: []StatusRole{RoleInitial}},
			"b": {Roles: []StatusRole{RoleStart}},
			"c": {Roles: []StatusRole{RoleDone}},
			"d": {Roles: []StatusRole{RoleDelete}},
		},
		Transitions: []WorkflowTransition{{FromStatus: "a", ToStatus: "b"}},
	}
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_DoneWithoutTerminal(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Statuses["completed"] = StatusConfig{Roles: []StatusRole{RoleDone}}
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_DeleteWithoutTerminal(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Statuses["deleted"] = StatusConfig{Roles: []StatusRole{RoleDelete}}
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_HighlightAndDim(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Statuses["active"] = StatusConfig{Roles: []StatusRole{RoleStart, RoleHighlight, RoleDim}}
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_OrphanTransition(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Transitions = append(workflow.Transitions, WorkflowTransition{FromStatus: "active", ToStatus: "ghost"})
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestValidateWorkflow_MissingInitialToStartTransition(test *testing.T) {
	workflow := baseValidWorkflow()
	workflow.Transitions = []WorkflowTransition{
		{FromStatus: "active", ToStatus: "completed"},
		{FromStatus: "active", ToStatus: "deleted"},
	}
	err := ValidateWorkflow(workflow)
	if !errors.Is(err, ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}
