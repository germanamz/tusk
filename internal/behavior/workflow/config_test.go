package workflow

import (
	"strings"
	"testing"
)

func TestDecodeConfig_AppliesToRequired(test *testing.T) {
	cfg := workflowConfig{
		States: []stateDecl{{Name: "open", Initial: true}},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "applies-to") {
		test.Errorf("validate: expected applies-to error, got %v", validateErr)
	}
}

func TestDecodeConfig_StatesRequired(test *testing.T) {
	cfg := workflowConfig{AppliesTo: []string{"ticket"}}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "states") {
		test.Errorf("validate: expected states error, got %v", validateErr)
	}
}

func TestDecodeConfig_DuplicateStateName(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "open"},
			{Name: "open"},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "duplicate") {
		test.Errorf("validate: expected duplicate error, got %v", validateErr)
	}
}

func TestDecodeConfig_MultipleInitial(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "a", Initial: true},
			{Name: "b", Initial: true},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "initial") {
		test.Errorf("validate: expected initial error, got %v", validateErr)
	}
}

func TestDecodeConfig_MultipleStart(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "a", Start: true},
			{Name: "b", Start: true},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "start") {
		test.Errorf("validate: expected start error, got %v", validateErr)
	}
}

func TestDecodeConfig_DoneWithoutTerminalRejected(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "shipped", Done: true, Terminal: false},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "terminal") {
		test.Errorf("validate: expected terminal-without-done error, got %v", validateErr)
	}
}

func TestDecodeConfig_TransitionReferencesUnknownState(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "a"},
		},
		Transitions: []transitionDecl{
			{From: "a", To: "missing"},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "missing") {
		test.Errorf("validate: expected unknown-state error, got %v", validateErr)
	}
}

func TestDecodeConfig_DuplicateTransition(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "a"},
			{Name: "b"},
		},
		Transitions: []transitionDecl{
			{From: "a", To: "b"},
			{From: "a", To: "b"},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "duplicate") {
		test.Errorf("validate: expected duplicate transition error, got %v", validateErr)
	}
}

func TestDecodeConfig_StatusPropertyDefaultsToStatus(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States:    []stateDecl{{Name: "open"}},
	}

	if cfg.normalize(); cfg.StatusProperty != "status" {
		test.Errorf("StatusProperty after normalize = %q, want %q", cfg.StatusProperty, "status")
	}
}

func TestDecodeConfig_HappyPath(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo:      []string{"ticket"},
		StatusProperty: "status",
		States: []stateDecl{
			{Name: "pending", Initial: true},
			{Name: "active", Start: true},
			{Name: "completed", Terminal: true, Done: true},
		},
		Transitions: []transitionDecl{
			{From: "pending", To: "active"},
			{From: "active", To: "completed"},
		},
	}

	if validateErr := cfg.validate(); validateErr != nil {
		test.Errorf("validate: %v", validateErr)
	}
}
