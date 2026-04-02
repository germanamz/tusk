package tui

import (
	"fmt"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
)

func TestFormatError_NotFound(t *testing.T) {
	err := fmt.Errorf("getting task: %w", domain.ErrNotFound)
	got := formatError(err, "abc12345")
	want := "Task not found: abc12345"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_Conflict(t *testing.T) {
	err := domain.ErrConflict
	got := formatError(err, "abc12345")
	want := "Version conflict - task was modified by another process"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_InvalidTransition(t *testing.T) {
	err := fmt.Errorf("transition %q → %q not allowed: %w", "pending", "completed", domain.ErrInvalidTransition)
	got := formatError(err, "abc12345")
	if got != err.Error() {
		t.Fatalf("expected original error message, got %q", got)
	}
}

func TestFormatError_Generic(t *testing.T) {
	err := fmt.Errorf("something went wrong")
	got := formatError(err, "abc12345")
	if got != "something went wrong" {
		t.Fatalf("expected original message, got %q", got)
	}
}
