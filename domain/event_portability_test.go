package domain_test

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
)

func TestWorkspaceImportedPayload_EventKind(t *testing.T) {
	p := domain.WorkspaceImportedPayload{
		Kind:          domain.EventWorkspaceImported,
		SchemaVersion: 1,
		SourceTuskVer: "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		Replace:       true,
		Truncate:      false,
		Counts:        map[string]int{"tasks": 42, "projects": 1},
	}
	if got := p.EventKind(); got != domain.EventWorkspaceImported {
		t.Fatalf("EventKind() = %q, want %q", got, domain.EventWorkspaceImported)
	}
}
