package service

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func defaultWeights() UrgencyWeights {
	return UrgencyWeights{
		Priority:    6.0,
		Due:         12.0,
		Age:         2.0,
		Active:      4.0,
		Blocking:    8.0,
		Blocked:     -5.0,
		Tags:        1.0,
		Project:     1.0,
		Annotations: 1.0,
		Waiting:     -3.0,
	}
}

func emptyContext() ScoringContext {
	return ScoringContext{
		BlockingCount:   map[uuid.UUID]int{},
		BlockedByCount:  map[uuid.UUID]int{},
		AnnotationCount: map[uuid.UUID]int{},
		TagCount:        map[uuid.UUID]int{},
		ProjectWeights:  map[uuid.UUID]*UrgencyWeights{},
	}
}

func TestUrgencyPriorityFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()

	tests := []struct {
		priority int
		want     float64
	}{
		{0, 0.0},
		{1, 1.5},
		{2, 3.0},
		{3, 4.5},
		{4, 6.0},
	}
	for _, tt := range tests {
		task := &domain.Task{ID: uuid.New(), Priority: tt.priority, Status: "pending", CreatedAt: time.Now()}
		got := engine.Score(task, ctx)
		// Score includes age and project factors too, so extract priority contribution
		baseline := engine.Score(&domain.Task{ID: uuid.New(), Priority: 0, Status: "pending", CreatedAt: task.CreatedAt}, ctx)
		contrib := got - baseline
		if diff := contrib - tt.want; diff > 0.01 || diff < -0.01 {
			t.Errorf("priority %d: got contribution %.2f, want %.2f", tt.priority, contrib, tt.want)
		}
	}
}

func TestUrgencyDueDateFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()
	now := time.Now()
	created := now.Add(-24 * time.Hour)

	// No due date: contribution is 0
	noDue := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created}
	baseScore := engine.Score(noDue, ctx)

	// Past due: high contribution
	pastDue := time.Now().Add(-48 * time.Hour)
	past := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created, DueAt: &pastDue}
	pastScore := engine.Score(past, ctx)
	if pastScore-baseScore < 10.0 {
		t.Errorf("past due contribution too low: %.2f", pastScore-baseScore)
	}

	// Due in 30 days: low contribution
	far := time.Now().Add(30 * 24 * time.Hour)
	farTask := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created, DueAt: &far}
	farScore := engine.Score(farTask, ctx)
	if farScore-baseScore > 3.0 {
		t.Errorf("far due contribution too high: %.2f", farScore-baseScore)
	}
}

func TestUrgencyActiveFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()
	created := time.Now()

	pending := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created}
	active := &domain.Task{ID: uuid.New(), Status: "active", CreatedAt: created}

	diff := engine.Score(active, ctx) - engine.Score(pending, ctx)
	if diff < 3.9 || diff > 4.1 {
		t.Errorf("active factor: got diff %.2f, want ~4.0", diff)
	}
}

func TestUrgencyBlockingFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	id := uuid.New()
	ctx := emptyContext()
	ctxBlocking := emptyContext()
	ctxBlocking.BlockingCount[id] = 2

	task := &domain.Task{ID: id, Status: "pending", CreatedAt: time.Now()}
	diff := engine.Score(task, ctxBlocking) - engine.Score(task, ctx)
	if diff < 7.9 || diff > 8.1 {
		t.Errorf("blocking factor: got diff %.2f, want ~8.0", diff)
	}
}

func TestUrgencyBlockedFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	id := uuid.New()
	ctx := emptyContext()
	ctxBlocked := emptyContext()
	ctxBlocked.BlockedByCount[id] = 1

	task := &domain.Task{ID: id, Status: "pending", CreatedAt: time.Now()}
	diff := engine.Score(task, ctxBlocked) - engine.Score(task, ctx)
	if diff > -4.9 || diff < -5.1 {
		t.Errorf("blocked factor: got diff %.2f, want ~-5.0", diff)
	}
}

func TestUrgencyWaitingFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()
	created := time.Now()

	noWait := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created}
	future := time.Now().Add(24 * time.Hour)
	waiting := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created, WaitUntil: &future}

	diff := engine.Score(waiting, ctx) - engine.Score(noWait, ctx)
	if diff > -2.9 || diff < -3.1 {
		t.Errorf("waiting factor: got diff %.2f, want ~-3.0", diff)
	}
}

func TestUrgencyScoreAndSort(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()
	now := time.Now()

	high := &domain.Task{ID: uuid.New(), Priority: 4, Status: "active", CreatedAt: now}
	low := &domain.Task{ID: uuid.New(), Priority: 0, Status: "pending", CreatedAt: now}
	mid := &domain.Task{ID: uuid.New(), Priority: 2, Status: "pending", CreatedAt: now}

	tasks := []*domain.Task{low, mid, high}
	engine.ScoreAndSort(tasks, ctx)

	if tasks[0].ID != high.ID {
		t.Error("expected highest urgency task first")
	}
	if tasks[2].ID != low.ID {
		t.Error("expected lowest urgency task last")
	}
	for _, task := range tasks {
		if task.Urgency == 0 && task.Priority > 0 {
			t.Errorf("task %s: urgency should be non-zero", task.ShortID)
		}
	}
}

func TestMergeWeights(t *testing.T) {
	defaults := defaultWeights()

	// Nil overrides returns defaults unchanged
	merged := MergeWeights(defaults, nil)
	if merged.Priority != 6.0 {
		t.Errorf("expected 6.0, got %.1f", merged.Priority)
	}

	// Override one field
	override := 20.0
	overrides := &domain.UrgencyOverrides{
		BlockingWeight: &override,
	}
	merged = MergeWeights(defaults, overrides)
	if merged.Blocking != 20.0 {
		t.Errorf("expected blocking 20.0, got %.1f", merged.Blocking)
	}
	if merged.Priority != 6.0 {
		t.Errorf("expected priority 6.0, got %.1f", merged.Priority)
	}
	if merged.Due != 12.0 {
		t.Errorf("expected due 12.0, got %.1f", merged.Due)
	}
}

func TestUrgencyProjectWeightOverride(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())

	overridePriority := 20.0
	defaultProjectID := uuid.New()
	customProjectID := uuid.New()
	ctx := ScoringContext{
		BlockingCount:   map[uuid.UUID]int{},
		BlockedByCount:  map[uuid.UUID]int{},
		AnnotationCount: map[uuid.UUID]int{},
		TagCount:        map[uuid.UUID]int{},
		ProjectWeights: map[uuid.UUID]*UrgencyWeights{
			customProjectID: {
				Priority:    overridePriority,
				Due:         12.0,
				Age:         2.0,
				Active:      4.0,
				Blocking:    8.0,
				Blocked:     -5.0,
				Tags:        1.0,
				Project:     1.0,
				Annotations: 1.0,
				Waiting:     -3.0,
			},
		},
	}

	defaultTask := &domain.Task{ID: uuid.New(), Priority: 4, Status: "pending", ProjectID: defaultProjectID, CreatedAt: time.Now()}
	customTask := &domain.Task{ID: uuid.New(), Priority: 4, Status: "pending", ProjectID: customProjectID, CreatedAt: time.Now()}

	defaultScore := engine.Score(defaultTask, ctx)
	customScore := engine.Score(customTask, ctx)

	if customScore <= defaultScore {
		t.Errorf("custom project should score higher (%.2f) than default (%.2f)", customScore, defaultScore)
	}
}

func TestUrgencyEngine_Reload(t *testing.T) {
	e := NewUrgencyEngine(UrgencyWeights{Priority: 10})
	task := &domain.Task{Priority: 4}
	ctx := ScoringContext{}

	before := e.Score(task, ctx)
	if before == 0 {
		t.Fatalf("expected non-zero score before reload")
	}

	e.Reload(UrgencyWeights{Priority: 0})
	after := e.Score(task, ctx)
	if after != 0 {
		t.Fatalf("expected zero score after reload, got %v", after)
	}
}
