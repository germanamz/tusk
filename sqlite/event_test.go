package sqlite

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// Compile-time check: *EventRepo must implement repository.EventRepository.
var _ repository.EventRepository = (*EventRepo)(nil)

// newTestTaskEvent builds a *domain.Task suitable for driving task event
// constructors. It generates fresh UUIDs and uses a current timestamp.
func newTestTaskEvent() *domain.Task {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Task{
		ID:        uuid.New(),
		ShortID:   uuid.New().String()[:8],
		ProjectID: uuid.New(),
		Title:     "event-test task",
		Status:    "pending",
		Priority:  2,
		CreatedAt: now,
	}
}

func newTestRelationForEvent() *domain.Relation {
	return &domain.Relation{
		ID:           uuid.New(),
		SourceID:     uuid.New(),
		TargetID:     uuid.New(),
		RelationType: "blocks",
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
}

// TestEventRepo_RecordAndListRoundtripAllPayloads verifies every payload
// constructor produces an event that can be recorded and read back with all
// fields intact.
func TestEventRepo_RecordAndListRoundtripAllPayloads(t *testing.T) {
	t.Parallel()

	actor := new("player-1")
	task := newTestTaskEvent()
	rel := newTestRelationForEvent()

	type testCase struct {
		name  string
		event *domain.Event
	}
	cases := []testCase{
		{"task_created", domain.NewTaskCreatedEvent(task, actor)},
		{"task_modified", domain.NewTaskModifiedEvent(task, map[string]domain.FieldChange{
			"title": {From: "old", To: "new"},
		}, actor)},
		{"status_changed", domain.NewStatusChangedEvent(task, "pending", "active", []string{"active"}, "user", actor)},
		{"task_started", domain.NewTaskStartedEvent(task, "pending", true, actor)},
		{"task_claimed", domain.NewTaskClaimedEvent(task, "player-2", actor)},
		{"task_released", domain.NewTaskReleasedEvent(task, "player-2", actor)},
		{"task_completed", domain.NewTaskCompletedEvent(task, "active", actor)},
		{"task_deleted", domain.NewTaskDeletedEvent(task, "pending", actor)},
		{"task_popped", domain.NewTaskPoppedEvent(task, "player-2", "active", actor)},
		{"relation_added", domain.NewRelationAddedEvent(rel, "src-1234", "tgt-5678", actor)},
		{"relation_removed", domain.NewRelationRemovedEvent(rel, "src-1234", "tgt-5678", actor)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := testStore(t)
			repo := NewEventRepo(s.DB(), 0, 0)
			ctx := context.Background()

			if err := repo.Record(ctx, tc.event); err != nil {
				t.Fatalf("Record: %v", err)
			}

			typ := tc.event.Type
			out, err := repo.List(ctx, repository.EventFilter{Type: &typ})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(out) != 1 {
				t.Fatalf("expected 1 event, got %d", len(out))
			}
			got := out[0]
			if got.ID != tc.event.ID {
				t.Errorf("ID mismatch: got %s, want %s", got.ID, tc.event.ID)
			}
			if got.Type != tc.event.Type {
				t.Errorf("Type mismatch: got %q, want %q", got.Type, tc.event.Type)
			}
			if got.EntityID != tc.event.EntityID {
				t.Errorf("EntityID mismatch: got %q, want %q", got.EntityID, tc.event.EntityID)
			}
			if got.EntityKind != tc.event.EntityKind {
				t.Errorf("EntityKind mismatch: got %q, want %q", got.EntityKind, tc.event.EntityKind)
			}
			if got.PlayerID == nil || *got.PlayerID != *tc.event.PlayerID {
				t.Errorf("PlayerID mismatch: got %v, want %v", got.PlayerID, tc.event.PlayerID)
			}
			if !got.CreatedAt.Equal(tc.event.CreatedAt) {
				t.Errorf("CreatedAt mismatch: got %v, want %v", got.CreatedAt, tc.event.CreatedAt)
			}
			if !reflect.DeepEqual(got.Payload, tc.event.Payload) {
				t.Errorf("Payload mismatch:\n got  %#v\n want %#v", got.Payload, tc.event.Payload)
			}
		})
	}
}

// TestEventRepo_ListFilters exercises every EventFilter field both
// individually and in combination.
func TestEventRepo_ListFilters(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	repo := NewEventRepo(s.DB(), 0, 0)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	actor := new("player-1")

	taskA := newTestTaskEvent()
	taskB := newTestTaskEvent()
	rel := newTestRelationForEvent()

	events := []*domain.Event{
		domain.NewTaskCreatedEvent(taskA, actor),
		domain.NewTaskModifiedEvent(taskA, map[string]domain.FieldChange{"title": {From: "a", To: "b"}}, actor),
		domain.NewTaskCreatedEvent(taskB, actor),
		domain.NewRelationAddedEvent(rel, "sa", "tb", actor),
	}
	for i, e := range events {
		e.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if err := repo.Record(ctx, e); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	taskKind := domain.EntityTask
	got, err := repo.List(ctx, repository.EventFilter{EntityKind: &taskKind})
	if err != nil {
		t.Fatalf("List by EntityKind: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("EntityKind filter: want 3, got %d", len(got))
	}

	taskAID := taskA.ID.String()
	got, err = repo.List(ctx, repository.EventFilter{EntityID: &taskAID})
	if err != nil {
		t.Fatalf("List by EntityID: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("EntityID filter: want 2, got %d", len(got))
	}

	typ := domain.EventTaskCreated
	got, err = repo.List(ctx, repository.EventFilter{Type: &typ})
	if err != nil {
		t.Fatalf("List by Type: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Type filter: want 2, got %d", len(got))
	}

	since := base.Add(1 * time.Second)
	got, err = repo.List(ctx, repository.EventFilter{Since: &since})
	if err != nil {
		t.Fatalf("List by Since: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Since filter: want 3, got %d", len(got))
	}

	until := base.Add(1 * time.Second)
	got, err = repo.List(ctx, repository.EventFilter{Until: &until})
	if err != nil {
		t.Fatalf("List by Until: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Until filter: want 2, got %d", len(got))
	}

	got, err = repo.List(ctx, repository.EventFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List by Limit: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Limit filter: want 2, got %d", len(got))
	}

	// Combined: task kind AND task_created AND within window AND limit.
	// Only the first event (taskA created at base) matches Type=task_created and Until=base+1s.
	got, err = repo.List(ctx, repository.EventFilter{
		EntityKind: &taskKind,
		Type:       &typ,
		Since:      &base,
		Until:      &until,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("List combined: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("combined filter: want 1, got %d", len(got))
	}
}

// TestEventRepo_ListOrdering verifies the returned slice is ordered by
// (created_at ASC, id ASC).
func TestEventRepo_ListOrdering(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	repo := NewEventRepo(s.DB(), 0, 0)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	actor := new("p")
	task := newTestTaskEvent()

	e1 := domain.NewTaskCreatedEvent(task, actor)
	e1.CreatedAt = base.Add(2 * time.Second)
	e2 := domain.NewTaskCreatedEvent(task, actor)
	e2.CreatedAt = base.Add(1 * time.Second)
	e3 := domain.NewTaskCreatedEvent(task, actor)
	e3.CreatedAt = base

	for _, e := range []*domain.Event{e1, e2, e3} {
		if err := repo.Record(ctx, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	got, err := repo.List(ctx, repository.EventFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		prev := got[i-1]
		cur := got[i]
		if cur.CreatedAt.Before(prev.CreatedAt) {
			t.Fatalf("not ordered: index %d before %d (%v before %v)", i, i-1, cur.CreatedAt, prev.CreatedAt)
		}
		if cur.CreatedAt.Equal(prev.CreatedAt) && cur.ID.String() < prev.ID.String() {
			t.Fatalf("tie-breaker by id broken at index %d", i)
		}
	}
}

// TestEventRepo_LazyPrune exercises the lazy retention policy.
// maxEvents=10, pruneSlack=3: threshold=13. Once the 14th row lands, the
// oldest 4 are deleted and the table stabilizes at 10 rows.
func TestEventRepo_LazyPrune(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	repo := NewEventRepo(s.DB(), 10, 3)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	task := newTestTaskEvent()
	actor := new("p")

	record := func(i int) {
		e := domain.NewTaskCreatedEvent(task, actor)
		e.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if err := repo.Record(ctx, e); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	for i := range 12 {
		record(i)
	}
	if n, _ := repo.Count(ctx); n != 12 {
		t.Fatalf("after 12 inserts: want count=12, got %d", n)
	}

	record(12) // 13th — count equals threshold, no prune
	if n, _ := repo.Count(ctx); n != 13 {
		t.Fatalf("after 13 inserts: want count=13, got %d", n)
	}

	record(13) // 14th — threshold exceeded, prune fires
	if n, _ := repo.Count(ctx); n != 10 {
		t.Fatalf("after 14 inserts: want count=10, got %d", n)
	}

	got, err := repo.List(ctx, repository.EventFilter{Limit: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	wantFirst := base.Add(4 * time.Second)
	if !got[0].CreatedAt.Equal(wantFirst) {
		t.Fatalf("oldest surviving event: want CreatedAt=%v, got %v", wantFirst, got[0].CreatedAt)
	}
}

// TestEventRepo_RetentionDisabled verifies maxEvents=0 means "keep everything".
func TestEventRepo_RetentionDisabled(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	repo := NewEventRepo(s.DB(), 0, 0)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	task := newTestTaskEvent()
	actor := new("p")

	for i := range 100 {
		e := domain.NewTaskCreatedEvent(task, actor)
		e.CreatedAt = base.Add(time.Duration(i) * time.Millisecond)
		if err := repo.Record(ctx, e); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}
	if n, _ := repo.Count(ctx); n != 100 {
		t.Fatalf("want 100, got %d", n)
	}
}

// TestEventRepo_PruneToSize verifies explicit pruning keeps the newest rows.
func TestEventRepo_PruneToSize(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	repo := NewEventRepo(s.DB(), 0, 0)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	task := newTestTaskEvent()
	actor := new("p")

	for i := range 20 {
		e := domain.NewTaskCreatedEvent(task, actor)
		e.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if err := repo.Record(ctx, e); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	deleted, err := repo.PruneToSize(ctx, 5)
	if err != nil {
		t.Fatalf("PruneToSize: %v", err)
	}
	if deleted != 15 {
		t.Fatalf("deleted: want 15, got %d", deleted)
	}
	if n, _ := repo.Count(ctx); n != 5 {
		t.Fatalf("after prune: want 5, got %d", n)
	}

	got, err := repo.List(ctx, repository.EventFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("list after prune: want 5, got %d", len(got))
	}
	for i, e := range got {
		want := base.Add(time.Duration(15+i) * time.Second)
		if !e.CreatedAt.Equal(want) {
			t.Fatalf("survivor[%d]: want CreatedAt=%v, got %v", i, want, e.CreatedAt)
		}
	}
}

// TestEventRepo_RecordRejectsMismatchedPayload verifies Record refuses to
// insert when Event.Type does not match Payload.EventKind().
func TestEventRepo_RecordRejectsMismatchedPayload(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	repo := NewEventRepo(s.DB(), 0, 0)
	ctx := context.Background()

	task := newTestTaskEvent()
	evt := domain.NewTaskCreatedEvent(task, new("p"))
	// Hand-crafted mismatch: Type says "task_modified" but payload kind is "task_created".
	evt.Type = domain.EventTaskModified

	if err := repo.Record(ctx, evt); err == nil {
		t.Fatal("expected error, got nil")
	}

	n, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 rows inserted, got %d", n)
	}
}
