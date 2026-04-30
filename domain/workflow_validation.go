// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package domain

import "fmt"

// ValidateWorkflow checks that a workflow satisfies the role-schema rules
// shared by both the config loader and the WorkflowService write path.
//
// Rules enforced:
//   - At least one status is defined.
//   - Each status role is a known StatusRole value.
//   - Exactly one status carries RoleInitial.
//   - Exactly one status carries RoleStart.
//   - At least one status carries RoleTerminal.
//   - Exactly one status carries RoleDone, and it must also be terminal.
//   - Exactly one status carries RoleDelete, and it must also be terminal.
//   - RoleHighlight and RoleDim are mutually exclusive on a status.
//   - Every transition references statuses that exist in the workflow.
//   - A transition initial → start exists.
//
// All failures are wrapped errors of ErrInvalidWorkflow so callers can match
// with errors.Is.
func ValidateWorkflow(workflow *Workflow) error {
	if workflow == nil {
		return fmt.Errorf("%w: workflow is nil", ErrInvalidWorkflow)
	}
	name := workflow.Name
	if len(workflow.Statuses) == 0 {
		return fmt.Errorf("%w: workflow %q must have at least one status", ErrInvalidWorkflow, name)
	}

	roleCounts := make(map[StatusRole]int)
	for statusName, statusConfig := range workflow.Statuses {
		for _, role := range statusConfig.Roles {
			if !validRole(role) {
				return fmt.Errorf("%w: workflow %q: status %q has unknown role %q", ErrInvalidWorkflow, name, statusName, role)
			}
			roleCounts[role]++
		}
	}

	if roleCounts[RoleInitial] != 1 {
		return fmt.Errorf("%w: workflow %q must have exactly one status with role %q (found %d)", ErrInvalidWorkflow, name, RoleInitial, roleCounts[RoleInitial])
	}
	if roleCounts[RoleStart] != 1 {
		return fmt.Errorf("%w: workflow %q must have exactly one status with role %q (found %d)", ErrInvalidWorkflow, name, RoleStart, roleCounts[RoleStart])
	}
	if roleCounts[RoleTerminal] < 1 {
		return fmt.Errorf("%w: workflow %q must have at least one status with role %q", ErrInvalidWorkflow, name, RoleTerminal)
	}
	if roleCounts[RoleDone] != 1 {
		return fmt.Errorf("%w: workflow %q must have exactly one status with role %q (found %d)", ErrInvalidWorkflow, name, RoleDone, roleCounts[RoleDone])
	}
	if roleCounts[RoleDelete] != 1 {
		return fmt.Errorf("%w: workflow %q must have exactly one status with role %q (found %d)", ErrInvalidWorkflow, name, RoleDelete, roleCounts[RoleDelete])
	}

	for statusName, statusConfig := range workflow.Statuses {
		var hasDone, hasDelete, hasTerminal, hasHighlight, hasDim bool
		for _, role := range statusConfig.Roles {
			switch role {
			case RoleDone:
				hasDone = true
			case RoleDelete:
				hasDelete = true
			case RoleTerminal:
				hasTerminal = true
			case RoleHighlight:
				hasHighlight = true
			case RoleDim:
				hasDim = true
			}
		}
		if hasDone && !hasTerminal {
			return fmt.Errorf("%w: workflow %q: status %q has role %q but missing required role %q", ErrInvalidWorkflow, name, statusName, RoleDone, RoleTerminal)
		}
		if hasDelete && !hasTerminal {
			return fmt.Errorf("%w: workflow %q: status %q has role %q but missing required role %q", ErrInvalidWorkflow, name, statusName, RoleDelete, RoleTerminal)
		}
		if hasHighlight && hasDim {
			return fmt.Errorf("%w: workflow %q: status %q cannot have both %q and %q roles", ErrInvalidWorkflow, name, statusName, RoleHighlight, RoleDim)
		}
	}

	for _, transition := range workflow.Transitions {
		if _, ok := workflow.Statuses[transition.FromStatus]; !ok {
			return fmt.Errorf("%w: workflow %q: transition references unknown status %q", ErrInvalidWorkflow, name, transition.FromStatus)
		}
		if _, ok := workflow.Statuses[transition.ToStatus]; !ok {
			return fmt.Errorf("%w: workflow %q: transition references unknown status %q", ErrInvalidWorkflow, name, transition.ToStatus)
		}
	}

	var initialStatus, startStatus string
	for statusName, statusConfig := range workflow.Statuses {
		for _, role := range statusConfig.Roles {
			if role == RoleInitial {
				initialStatus = statusName
			}
			if role == RoleStart {
				startStatus = statusName
			}
		}
	}
	for _, transition := range workflow.Transitions {
		if transition.FromStatus == initialStatus && transition.ToStatus == startStatus {
			return nil
		}
	}
	return fmt.Errorf("%w: workflow %q: no transition from %q (%s) to %q (%s)", ErrInvalidWorkflow, name, initialStatus, RoleInitial, startStatus, RoleStart)
}

func validRole(role StatusRole) bool {
	switch role {
	case RoleInitial, RoleStart, RoleTerminal, RoleDone, RoleDelete, RoleHighlight, RoleDim:
		return true
	}
	return false
}
