package domain_test

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func TestTypesCompile(test *testing.T) {
	now := time.Now()
	id := uuid.New()
	priority := 3
	waiting := true

	_ = domain.Task{
		ID:             id,
		ShortID:        "a3f8b2c1",
		ParentID:       &id,
		ProjectID:      domain.DefaultProjectUUID,
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
		Name:       "backend",
		WorkflowID: id,
	}

	color := "#ff0000"
	_ = domain.Tag{
		ID:    id,
		Name:  "bug",
		Color: &color,
	}

	_ = domain.Workflow{
		Name: "default",
		Statuses: map[string]domain.StatusConfig{
			"pending":   {},
			"active":    {},
			"completed": {},
			"deleted":   {},
		},
		Transitions: []domain.WorkflowTransition{
			{FromStatus: "pending", ToStatus: "active"},
		},
	}

	_ = domain.WorkflowTransition{
		FromStatus: "pending",
		ToStatus:   "active",
	}

	projID := domain.DefaultProjectUUID
	_ = domain.TaskFilter{
		ProjectID:   &projID,
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

func TestSentinelErrors(test *testing.T) {
	sentinels := []error{
		domain.ErrNotFound,
		domain.ErrConflict,
		domain.ErrCyclicBlock,
		domain.ErrInvalidTransition,
		domain.ErrDuplicateRelation,
	}
	for _, sentinel := range sentinels {
		if sentinel == nil {
			test.Fatal("sentinel error is nil")
		}
		if sentinel.Error() == "" {
			test.Fatal("sentinel error has empty message")
		}
	}
}
