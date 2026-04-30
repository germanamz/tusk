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
func TestEventRepo_RecordAndListRoundtripAllPayloads(test *testing.T) {
	test.Parallel()

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

	for _, caseItem := range cases {
		test.Run(caseItem.name, func(test *testing.T) {
			test.Parallel()

			store := testStore(test)
			repo := NewEventRepo(store.DB(), 0, 0)
			ctx := context.Background()

			if err := repo.Record(ctx, caseItem.event); err != nil {
				test.Fatalf("Record: %v", err)
			}

			typ := caseItem.event.Type
			out, err := repo.List(ctx, repository.EventFilter{Type: &typ})

			if err != nil {
				test.Fatalf("List: %v", err)
			}

			if len(out) != 1 {
				test.Fatalf("expected 1 event, got %d", len(out))
			}

			got := out[0]

			if got.ID != caseItem.event.ID {
				test.Errorf("ID mismatch: got %s, want %s", got.ID, caseItem.event.ID)
			}

			if got.Type != caseItem.event.Type {
				test.Errorf("Type mismatch: got %q, want %q", got.Type, caseItem.event.Type)
			}

			if got.EntityID != caseItem.event.EntityID {
				test.Errorf("EntityID mismatch: got %q, want %q", got.EntityID, caseItem.event.EntityID)
			}

			if got.EntityKind != caseItem.event.EntityKind {
				test.Errorf("EntityKind mismatch: got %q, want %q", got.EntityKind, caseItem.event.EntityKind)
			}

			if got.PlayerID == nil || *got.PlayerID != *caseItem.event.PlayerID {
				test.Errorf("PlayerID mismatch: got %v, want %v", got.PlayerID, caseItem.event.PlayerID)
			}

			if !got.CreatedAt.Equal(caseItem.event.CreatedAt) {
				test.Errorf("CreatedAt mismatch: got %v, want %v", got.CreatedAt, caseItem.event.CreatedAt)
			}

			if !reflect.DeepEqual(got.Payload, caseItem.event.Payload) {
				test.Errorf("Payload mismatch:\n got  %#v\n want %#v", got.Payload, caseItem.event.Payload)
			}
		})
	}
}

// TestEventRepo_ListFilters exercises every EventFilter field both
// individually and in combination.
func TestEventRepo_ListFilters(test *testing.T) {
	test.Parallel()

	store := testStore(test)
	repo := NewEventRepo(store.DB(), 0, 0)
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

	for index, event := range events {
		event.CreatedAt = base.Add(time.Duration(index) * time.Second)

		if err := repo.Record(ctx, event); err != nil {
			test.Fatalf("Record[%d]: %v", index, err)
		}
	}

	taskKind := domain.EntityTask
	got, listByKindErr := repo.List(ctx, repository.EventFilter{EntityKind: &taskKind})

	if listByKindErr != nil {
		test.Fatalf("List by EntityKind: %v", listByKindErr)
	}

	if len(got) != 3 {
		test.Fatalf("EntityKind filter: want 3, got %d", len(got))
	}

	taskAID := taskA.ID.String()
	got, listByEntityIDErr := repo.List(ctx, repository.EventFilter{EntityID: &taskAID})

	if listByEntityIDErr != nil {
		test.Fatalf("List by EntityID: %v", listByEntityIDErr)
	}

	if len(got) != 2 {
		test.Fatalf("EntityID filter: want 2, got %d", len(got))
	}

	typ := domain.EventTaskCreated
	got, listByTypeErr := repo.List(ctx, repository.EventFilter{Type: &typ})

	if listByTypeErr != nil {
		test.Fatalf("List by Type: %v", listByTypeErr)
	}

	if len(got) != 2 {
		test.Fatalf("Type filter: want 2, got %d", len(got))
	}

	since := base.Add(1 * time.Second)
	got, listBySinceErr := repo.List(ctx, repository.EventFilter{Since: &since})

	if listBySinceErr != nil {
		test.Fatalf("List by Since: %v", listBySinceErr)
	}

	if len(got) != 3 {
		test.Fatalf("Since filter: want 3, got %d", len(got))
	}

	until := base.Add(1 * time.Second)
	got, listByUntilErr := repo.List(ctx, repository.EventFilter{Until: &until})

	if listByUntilErr != nil {
		test.Fatalf("List by Until: %v", listByUntilErr)
	}

	if len(got) != 2 {
		test.Fatalf("Until filter: want 2, got %d", len(got))
	}

	got, listByLimitErr := repo.List(ctx, repository.EventFilter{Limit: 2})

	if listByLimitErr != nil {
		test.Fatalf("List by Limit: %v", listByLimitErr)
	}

	if len(got) != 2 {
		test.Fatalf("Limit filter: want 2, got %d", len(got))
	}

	// Combined: task kind AND task_created AND within window AND limit.
	// Only the first event (taskA created at base) matches Type=task_created and Until=base+1s.
	got, listCombinedErr := repo.List(ctx, repository.EventFilter{
		EntityKind: &taskKind,
		Type:       &typ,
		Since:      &base,
		Until:      &until,
		Limit:      10,
	})

	if listCombinedErr != nil {
		test.Fatalf("List combined: %v", listCombinedErr)
	}

	if len(got) != 1 {
		test.Fatalf("combined filter: want 1, got %d", len(got))
	}
}

// TestEventRepo_ListOrdering verifies the returned slice is ordered by
// (created_at ASC, id ASC).
func TestEventRepo_ListOrdering(test *testing.T) {
	test.Parallel()

	store := testStore(test)
	repo := NewEventRepo(store.DB(), 0, 0)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	actor := new("p")
	task := newTestTaskEvent()

	event1 := domain.NewTaskCreatedEvent(task, actor)
	event1.CreatedAt = base.Add(2 * time.Second)
	event2 := domain.NewTaskCreatedEvent(task, actor)
	event2.CreatedAt = base.Add(1 * time.Second)
	event3 := domain.NewTaskCreatedEvent(task, actor)
	event3.CreatedAt = base

	for _, event := range []*domain.Event{event1, event2, event3} {
		if err := repo.Record(ctx, event); err != nil {
			test.Fatalf("Record: %v", err)
		}
	}

	got, err := repo.List(ctx, repository.EventFilter{})

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(got) != 3 {
		test.Fatalf("want 3, got %d", len(got))
	}

	for index := 1; index < len(got); index++ {
		prev := got[index-1]
		cur := got[index]

		if cur.CreatedAt.Before(prev.CreatedAt) {
			test.Fatalf("not ordered: index %d before %d (%v before %v)", index, index-1, cur.CreatedAt, prev.CreatedAt)
		}

		if cur.CreatedAt.Equal(prev.CreatedAt) && cur.ID.String() < prev.ID.String() {
			test.Fatalf("tie-breaker by id broken at index %d", index)
		}
	}
}

// TestEventRepo_LazyPrune exercises the lazy retention policy.
// maxEvents=10, pruneSlack=3: threshold=13. Once the 14th row lands, the
// oldest 4 are deleted and the table stabilizes at 10 rows.
func TestEventRepo_LazyPrune(test *testing.T) {
	test.Parallel()

	store := testStore(test)
	repo := NewEventRepo(store.DB(), 10, 3)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	task := newTestTaskEvent()
	actor := new("p")

	record := func(index int) {
		event := domain.NewTaskCreatedEvent(task, actor)
		event.CreatedAt = base.Add(time.Duration(index) * time.Second)

		if err := repo.Record(ctx, event); err != nil {
			test.Fatalf("Record[%d]: %v", index, err)
		}
	}

	for index := range 12 {
		record(index)
	}

	if count, _ := repo.Count(ctx); count != 12 {
		test.Fatalf("after 12 inserts: want count=12, got %d", count)
	}

	record(12) // 13th — count equals threshold, no prune

	if count, _ := repo.Count(ctx); count != 13 {
		test.Fatalf("after 13 inserts: want count=13, got %d", count)
	}

	record(13) // 14th — threshold exceeded, prune fires

	if count, _ := repo.Count(ctx); count != 10 {
		test.Fatalf("after 14 inserts: want count=10, got %d", count)
	}

	got, err := repo.List(ctx, repository.EventFilter{Limit: 1})

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(got) != 1 {
		test.Fatalf("want 1, got %d", len(got))
	}

	wantFirst := base.Add(4 * time.Second)

	if !got[0].CreatedAt.Equal(wantFirst) {
		test.Fatalf("oldest surviving event: want CreatedAt=%v, got %v", wantFirst, got[0].CreatedAt)
	}
}

// TestEventRepo_RetentionDisabled verifies maxEvents=0 means "keep everything".
func TestEventRepo_RetentionDisabled(test *testing.T) {
	test.Parallel()

	store := testStore(test)
	repo := NewEventRepo(store.DB(), 0, 0)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	task := newTestTaskEvent()
	actor := new("p")

	for index := range 100 {
		event := domain.NewTaskCreatedEvent(task, actor)
		event.CreatedAt = base.Add(time.Duration(index) * time.Millisecond)

		if err := repo.Record(ctx, event); err != nil {
			test.Fatalf("Record[%d]: %v", index, err)
		}
	}

	if count, _ := repo.Count(ctx); count != 100 {
		test.Fatalf("want 100, got %d", count)
	}
}

// TestEventRepo_PruneToSize verifies explicit pruning keeps the newest rows.
func TestEventRepo_PruneToSize(test *testing.T) {
	test.Parallel()

	store := testStore(test)
	repo := NewEventRepo(store.DB(), 0, 0)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	task := newTestTaskEvent()
	actor := new("p")

	for index := range 20 {
		event := domain.NewTaskCreatedEvent(task, actor)
		event.CreatedAt = base.Add(time.Duration(index) * time.Second)

		if err := repo.Record(ctx, event); err != nil {
			test.Fatalf("Record[%d]: %v", index, err)
		}
	}

	deleted, pruneErr := repo.PruneToSize(ctx, 5)

	if pruneErr != nil {
		test.Fatalf("PruneToSize: %v", pruneErr)
	}

	if deleted != 15 {
		test.Fatalf("deleted: want 15, got %d", deleted)
	}

	if count, _ := repo.Count(ctx); count != 5 {
		test.Fatalf("after prune: want 5, got %d", count)
	}

	got, listErr := repo.List(ctx, repository.EventFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(got) != 5 {
		test.Fatalf("list after prune: want 5, got %d", len(got))
	}

	for index, event := range got {
		want := base.Add(time.Duration(15+index) * time.Second)

		if !event.CreatedAt.Equal(want) {
			test.Fatalf("survivor[%d]: want CreatedAt=%v, got %v", index, want, event.CreatedAt)
		}
	}
}

// TestEventRepo_RecordRejectsMismatchedPayload verifies Record refuses to
// insert when Event.Type does not match Payload.EventKind().
func TestEventRepo_RecordRejectsMismatchedPayload(test *testing.T) {
	test.Parallel()

	store := testStore(test)
	repo := NewEventRepo(store.DB(), 0, 0)
	ctx := context.Background()

	task := newTestTaskEvent()
	event := domain.NewTaskCreatedEvent(task, new("p"))
	// Hand-crafted mismatch: Type says "task_modified" but payload kind is "task_created".
	event.Type = domain.EventTaskModified

	if err := repo.Record(ctx, event); err == nil {
		test.Fatal("expected error, got nil")
	}

	count, countErr := repo.Count(ctx)

	if countErr != nil {
		test.Fatalf("Count: %v", countErr)
	}

	if count != 0 {
		test.Fatalf("want 0 rows inserted, got %d", count)
	}
}
