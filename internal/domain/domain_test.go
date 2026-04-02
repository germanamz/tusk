package domain_test

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

func TestTypesCompile(t *testing.T) {
	now := time.Now()
	id := uuid.New()
	priority := 3
	waiting := true

	_ = domain.Task{
		ID:             id,
		ShortID:        "a3f8b2c1",
		ParentID:       &id,
		ProjectID:      &id,
		Title:          "test",
		Description:    "desc",
		Status:         "pending",
		Priority:       priority,
		Version:        1,
		DueAt:          &now,
		WaitUntil:      &now,
		RecurrenceRule: nil,
		UDA:            map[string]any{"key": "val"},
		CreatedAt:      now,
		ModifiedAt:     now,
	}

	_ = domain.Annotation{
		ID:        id,
		TaskID:    id,
		Body:      "note",
		CreatedAt: now,
	}

	_ = domain.Relation{
		ID:           id,
		SourceID:     id,
		TargetID:     id,
		RelationType: "blocks",
		CreatedAt:    now,
	}

	_ = domain.Project{
		ID:              id,
		Name:            "backend",
		Description:     "desc",
		DefaultWorkflow: "default",
		CreatedAt:       now,
	}

	color := "#ff0000"
	_ = domain.Tag{
		ID:    id,
		Name:  "bug",
		Color: &color,
	}

	_ = domain.Workflow{
		ID:        id,
		ProjectID: id,
		Name:      "default",
		Statuses:  []string{"pending", "active", "completed", "deleted"},
	}

	_ = domain.WorkflowTransition{
		ID:         id,
		WorkflowID: id,
		FromStatus: "pending",
		ToStatus:   "active",
	}

	_ = domain.TaskFilter{
		ProjectID:   &id,
		ParentID:    &id,
		RootID:      &id,
		Statuses:    []string{"pending"},
		Tags:        []string{"bug"},
		ExcludeTags: []string{"docs"},
		PriorityMin: &priority,
		PriorityMax: &priority,
		DueAfter:    &now,
		DueBefore:   &now,
		WaitingOnly: &waiting,
	}
}

func TestSentinelErrors(t *testing.T) {
	errors := []error{
		domain.ErrNotFound,
		domain.ErrConflict,
		domain.ErrCyclicBlock,
		domain.ErrInvalidTransition,
		domain.ErrDuplicateRelation,
	}
	for _, err := range errors {
		if err == nil {
			t.Fatal("sentinel error is nil")
		}
		if err.Error() == "" {
			t.Fatal("sentinel error has empty message")
		}
	}
}
