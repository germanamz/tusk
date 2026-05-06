// Package workflow implements the v1 reference behavior pack: declarative
// state-machine validation hooked to OnNodeWriteValidate.
package workflow

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/behavior"
)

// ErrorCode enumerates the rejection codes the workflow pack returns.
type ErrorCode string

const (
	ErrIllegalTransition  ErrorCode = "illegal-transition"
	ErrUnknownTargetState ErrorCode = "unknown-target-state"
	ErrNonInitialOnCreate ErrorCode = "non-initial-on-create"
	ErrCannotUnsetStatus  ErrorCode = "cannot-unset-status"
)

// Error is an outright rejection. The Modify path returns it; reindex
// captures it as a drift row.
type Error struct {
	Code         ErrorCode
	Property     string
	From         string   // current state; "" when setting for the first time
	To           string   // target state; "" when unsetting
	KnownStates  []string // populated for ErrUnknownTargetState
	ValidTargets []string // populated for ErrIllegalTransition
	PackInstance string   // e.g. "tickets"
}

func (err *Error) Error() string {
	header := fmt.Sprintf("workflow %q", err.PackInstance)

	switch err.Code {
	case ErrIllegalTransition:
		return fmt.Sprintf("%s: cannot transition %s %q → %q\n  valid targets from %q: %s",
			header, err.Property, err.From, err.To, err.From, joinOrEmpty(err.ValidTargets))

	case ErrUnknownTargetState:
		return fmt.Sprintf("%s: %q is not a declared state for property %q\n  declared states: %s",
			header, err.To, err.Property, joinOrEmpty(err.KnownStates))

	case ErrNonInitialOnCreate:
		return fmt.Sprintf("%s: %s must be set to an initial state on create\n  initial state(s): %s; got: %q",
			header, err.Property, joinOrEmpty(err.KnownStates), err.To)

	case ErrCannotUnsetStatus:
		return fmt.Sprintf("%s: cannot unset managed property %q (currently %q)\n  edit the file directly to remove the field, then reindex",
			header, err.Property, err.From)
	}

	return fmt.Sprintf("%s: unknown workflow error", header)
}

// RecoveredError is orphan-state recovery: returned when the validator
// allows a transition out of a status the manifest no longer declares.
// It implements behavior.Recoverable.
type RecoveredError struct {
	Property     string
	From         string
	To           string
	PackInstance string
}

func (err *RecoveredError) Error() string {
	return fmt.Sprintf("workflow %q recovered from unknown status %q → %q; transition not validated",
		err.PackInstance, err.From, err.To)
}

// AsRecoveredEvent satisfies behavior.Recoverable. The engine populates
// PackKind from the HookContext at fire time; PackInstance carried on
// the error is reaffirmed for symmetry.
func (err *RecoveredError) AsRecoveredEvent(packKind, packInstance string) behavior.RecoveredEvent {
	return behavior.RecoveredEvent{
		PackKind:     packKind,
		PackInstance: packInstance,
		Property:     err.Property,
		From:         err.From,
		To:           err.To,
		Message:      err.Error(),
	}
}

func joinOrEmpty(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}

	return strings.Join(values, ", ")
}
