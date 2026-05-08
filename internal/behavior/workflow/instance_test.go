package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/node"
)

func newTestInstance(test *testing.T) *instance {
	test.Helper()

	cfg := workflowConfig{
		AppliesTo:      []string{"ticket"},
		StatusProperty: "status",
		States: []stateDecl{
			{Name: "pending", Initial: true},
			{Name: "active"},
			{Name: "completed", Terminal: true, Done: true},
		},
		Transitions: []transitionDecl{
			{From: "pending", To: "active"},
			{From: "active", To: "completed"},
			{From: "active", To: "pending"},
		},
	}

	cfg.normalize()

	if validateErr := cfg.validate(); validateErr != nil {
		test.Fatalf("config validate: %v", validateErr)
	}

	return newInstance("tickets", cfg)
}

func makeNode(typ string, props map[string]any) *node.Node {
	return &node.Node{Type: typ, Properties: props}
}

func TestValidate_TypeOutsideAppliesToReturnsNil(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, nil, makeNode("note", nil))

	if err != nil {
		test.Errorf("validate: type outside applies-to should be no-op, got %v", err)
	}
}

func TestValidate_BothSidesEmptyReturnsNil(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, makeNode("ticket", map[string]any{}), makeNode("ticket", map[string]any{}))

	if err != nil {
		test.Errorf("validate: both sides empty should be no-op, got %v", err)
	}
}

func TestValidate_SetFromEmptyMustBeInitial(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, nil, makeNode("ticket", map[string]any{"status": "active"}))

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrNonInitialOnCreate {
		test.Errorf("validate: expected non-initial-on-create, got %v", err)
	}
}

func TestValidate_SetFromEmptyToInitialAccepted(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, nil, makeNode("ticket", map[string]any{"status": "pending"}))

	if err != nil {
		test.Errorf("validate: pending is initial, expected nil, got %v", err)
	}
}

func TestValidate_SetFromEmptyToUnknownState(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, nil, makeNode("ticket", map[string]any{"status": "donee"}))

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrUnknownTargetState {
		test.Errorf("validate: expected unknown-target-state, got %v", err)
	}
}

func TestValidate_UnsetRejected(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "active"})
	after := makeNode("ticket", map[string]any{})

	err := inst.validate(ctx, before, after)

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrCannotUnsetStatus {
		test.Errorf("validate: expected cannot-unset-status, got %v", err)
	}
}

func TestValidate_OrphanRecoveryToDeclared(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "blocked"})
	after := makeNode("ticket", map[string]any{"status": "active"})

	err := inst.validate(ctx, before, after)

	var recovered *RecoveredError

	if !errors.As(err, &recovered) {
		test.Errorf("validate: expected RecoveredError, got %v", err)

		return
	}

	if recovered.From != "blocked" || recovered.To != "active" || recovered.Property != "status" {
		test.Errorf("RecoveredError fields = %+v", recovered)
	}
}

func TestValidate_OrphanToUnknownTargetIsHardError(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "blocked"})
	after := makeNode("ticket", map[string]any{"status": "alsoBogus"})

	err := inst.validate(ctx, before, after)

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrUnknownTargetState {
		test.Errorf("validate: expected unknown-target-state, got %v", err)
	}
}

func TestValidate_NoOpSelfTransitionAllowed(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "active"})
	after := makeNode("ticket", map[string]any{"status": "active"})

	if err := inst.validate(ctx, before, after); err != nil {
		test.Errorf("validate: self-transition should be allowed, got %v", err)
	}
}

func TestValidate_LegalTransition(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "active"})
	after := makeNode("ticket", map[string]any{"status": "completed"})

	if err := inst.validate(ctx, before, after); err != nil {
		test.Errorf("validate: legal transition rejected: %v", err)
	}
}

func TestValidate_IllegalTransition(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "pending"})
	after := makeNode("ticket", map[string]any{"status": "completed"})

	err := inst.validate(ctx, before, after)

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrIllegalTransition {
		test.Errorf("validate: expected illegal-transition, got %v", err)
	}

	// ValidTargets should list "active" (the only legal next-state from "pending").
	if !strings.Contains(strings.Join(workflowErr.ValidTargets, ","), "active") {
		test.Errorf("ValidTargets = %v, want to include 'active'", workflowErr.ValidTargets)
	}
}

func TestValidate_NormalUnknownTarget(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "active"})
	after := makeNode("ticket", map[string]any{"status": "donee"})

	err := inst.validate(ctx, before, after)

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrUnknownTargetState {
		test.Errorf("validate: expected unknown-target-state, got %v", err)
	}
}
