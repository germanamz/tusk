package sqlite

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

var _ repository.WorkflowRepository = (*WorkflowRepo)(nil)

func TestWorkflowGetByProjectAndName(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	wf, err := repo.GetByProjectAndName(ctx, "default", "kanban")
	if err != nil {
		t.Fatalf("GetByProjectAndName: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected kanban, got %s", wf.Name)
	}
	if len(wf.Statuses) != 4 {
		t.Fatalf("expected 4 statuses, got %d", len(wf.Statuses))
	}
	expected := []string{"pending", "active", "completed", "deleted"}
	for i, s := range expected {
		if wf.Statuses[i] != s {
			t.Fatalf("status[%d]: expected %s, got %s", i, s, wf.Statuses[i])
		}
	}
}

func TestWorkflowGetByProjectAndNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	_, err := repo.GetByProjectAndName(context.Background(), "nonexistent-project", "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWorkflowGetTransitions(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	wf, err := repo.GetByProjectAndName(ctx, "default", "kanban")
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 6 {
		t.Fatalf("expected 6, got %d", len(transitions))
	}
}

func TestWorkflowGetTransitionsValidatePairs(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	wf, err := repo.GetByProjectAndName(ctx, "default", "kanban")
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Verify specific known transitions exist in the seed data.
	type pair struct{ from, to string }
	expected := map[pair]bool{
		{"pending", "active"}:    false,
		{"active", "completed"}:  false,
		{"active", "pending"}:    false,
		{"completed", "pending"}: false,
	}
	for _, tr := range transitions {
		p := pair{tr.FromStatus, tr.ToStatus}
		if _, ok := expected[p]; ok {
			expected[p] = true
		}
	}
	for p, found := range expected {
		if !found {
			t.Errorf("expected transition %s → %s not found", p.from, p.to)
		}
	}
}

func TestWorkflowCreateEmptyStatuses(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	wf := &domain.Workflow{ID: uuid.New(), ProjectID: "empty-statuses-proj", Name: "minimal", Statuses: []string{}}
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create with empty statuses: %v", err)
	}
	got, err := repo.GetByProjectAndName(ctx, "empty-statuses-proj", "minimal")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Statuses) != 0 {
		t.Fatalf("expected 0 statuses, got %d", len(got.Statuses))
	}
}

func TestWorkflowCreate(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	wf := &domain.Workflow{ID: uuid.New(), ProjectID: "kanban-project", Name: "kanban", Statuses: []string{"backlog", "in_progress", "review", "done"}}
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByProjectAndName(ctx, "kanban-project", "kanban")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Statuses) != 4 {
		t.Fatalf("expected 4, got %d", len(got.Statuses))
	}
	if got.Statuses[0] != "backlog" {
		t.Fatalf("expected backlog, got %s", got.Statuses[0])
	}
}

func TestWorkflowAddTransition(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	wf := &domain.Workflow{ID: uuid.New(), ProjectID: "test-proj", Name: "simple", Statuses: []string{"open", "closed"}}
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}
	tr := &domain.WorkflowTransition{ID: uuid.New(), WorkflowID: wf.ID, FromStatus: "open", ToStatus: "closed"}
	if err := repo.AddTransition(ctx, tr); err != nil {
		t.Fatalf("AddTransition: %v", err)
	}
	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("expected 1, got %d", len(transitions))
	}
	if transitions[0].FromStatus != "open" || transitions[0].ToStatus != "closed" {
		t.Fatalf("unexpected: %s → %s", transitions[0].FromStatus, transitions[0].ToStatus)
	}
}
