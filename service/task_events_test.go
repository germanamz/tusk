package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/sqlite"
)

// listAllEvents returns every event recorded in the store. Tests assert on the
// subset of types they care about via countByType.
func listAllEvents(t *testing.T, store *sqlite.Store) []*domain.Event {
	t.Helper()
	repo := sqlite.NewEventRepo(store.DB(), 10000, 1000)
	events, err := repo.List(context.Background(), repository.EventFilter{})
	if err != nil {
		t.Fatalf("listing events: %v", err)
	}
	return events
}

// countByType groups events by Type so assertions do not depend on the total
// event count — adding a new event kind elsewhere should not fail these tests.
func countByType(events []*domain.Event) map[domain.EventType]int {
	counts := make(map[domain.EventType]int)
	for _, e := range events {
		counts[e.Type]++
	}
	return counts
}

// firstEventOfType returns the first event of the given type, or fails the test.
func firstEventOfType(t *testing.T, events []*domain.Event, typ domain.EventType) *domain.Event {
	t.Helper()
	for _, e := range events {
		if e.Type == typ {
			return e
		}
	}
	t.Fatalf("no event of type %q found among %d events", typ, len(events))
	return nil
}

// registerTestPlayer inserts a player row through the default bundle so Claim
// calls find a valid player record.
func registerTestPlayer(t *testing.T, env *testEnv, id string) {
	t.Helper()
	repo := sqlite.NewPlayerRepo(env.store.DB())
	svc := NewPlayerService(repo)
	if _, err := svc.Register(context.Background(), id, "agent"); err != nil {
		t.Fatalf("registering player %q: %v", id, err)
	}
}

func TestEvents_Create_EmitsTaskCreated(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)

	ctx := WithActor(context.Background(), "german")
	task := newMinimalTask("Write event tests")
	if err := env.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	events := listAllEvents(t, env.store)
	counts := countByType(events)
	if counts[domain.EventTaskCreated] != 1 {
		t.Fatalf("expected 1 task_created, got %d (all=%v)", counts[domain.EventTaskCreated], counts)
	}
	evt := firstEventOfType(t, events, domain.EventTaskCreated)
	if evt.EntityID != task.ID.String() {
		t.Fatalf("entity_id: got %q, want %q", evt.EntityID, task.ID.String())
	}
	if evt.EntityKind != domain.EntityTask {
		t.Fatalf("entity_kind: got %q, want %q", evt.EntityKind, domain.EntityTask)
	}
	if evt.PlayerID == nil || *evt.PlayerID != "german" {
		t.Fatalf("player_id: got %v, want *\"german\"", evt.PlayerID)
	}
	payload, ok := evt.Payload.(domain.TaskCreatedPayload)
	if !ok {
		t.Fatalf("payload: got %T, want TaskCreatedPayload", evt.Payload)
	}
	if payload.Title != "Write event tests" {
		t.Fatalf("payload.title: got %q, want %q", payload.Title, "Write event tests")
	}
}

func TestEvents_Create_NoActor_PlayerIDNil(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)

	task := newMinimalTask("No actor")
	if err := env.taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	evt := firstEventOfType(t, listAllEvents(t, env.store), domain.EventTaskCreated)
	if evt.PlayerID != nil {
		t.Fatalf("player_id: got %v, want nil", *evt.PlayerID)
	}
}

func TestEvents_Update_OnlyNonStatusFields_EmitsTaskModified(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := WithActor(context.Background(), "german")

	task := newMinimalTask("priority test")
	mustCreateTask(t, env.taskSvc, task)

	newPri := 4
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  task.ShortID,
		Version:  task.Version,
		Priority: &newPri,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	events := listAllEvents(t, env.store)
	counts := countByType(events)
	if counts[domain.EventTaskModified] != 1 {
		t.Fatalf("expected 1 task_modified, got %d (all=%v)", counts[domain.EventTaskModified], counts)
	}
	if counts[domain.EventStatusChanged] != 0 {
		t.Fatalf("expected 0 status_changed, got %d", counts[domain.EventStatusChanged])
	}
	evt := firstEventOfType(t, events, domain.EventTaskModified)
	payload, ok := evt.Payload.(domain.TaskModifiedPayload)
	if !ok {
		t.Fatalf("payload: got %T, want TaskModifiedPayload", evt.Payload)
	}
	if _, hasPri := payload.Changes["priority"]; !hasPri {
		t.Fatalf("changes should include 'priority', got keys=%v", keysOf(payload.Changes))
	}
	if _, hasStatus := payload.Changes["status"]; hasStatus {
		t.Fatalf("changes must not include 'status' — status flows via status_changed")
	}
}

func TestEvents_Update_OnlyStatus_EmitsStatusChangedUser(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("status test")
	mustCreateTask(t, env.taskSvc, task)

	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		Status:  ptr("active"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	events := listAllEvents(t, env.store)
	counts := countByType(events)
	if counts[domain.EventStatusChanged] != 1 {
		t.Fatalf("expected 1 status_changed, got %d", counts[domain.EventStatusChanged])
	}
	if counts[domain.EventTaskModified] != 0 {
		t.Fatalf("expected 0 task_modified, got %d", counts[domain.EventTaskModified])
	}
	evt := firstEventOfType(t, events, domain.EventStatusChanged)
	payload, ok := evt.Payload.(domain.StatusChangedPayload)
	if !ok {
		t.Fatalf("payload: got %T, want StatusChangedPayload", evt.Payload)
	}
	if payload.Source != "user" {
		t.Fatalf("source: got %q, want \"user\"", payload.Source)
	}
	if payload.From != "pending" || payload.To != "active" {
		t.Fatalf("from/to: got %q → %q, want pending → active", payload.From, payload.To)
	}
}

func TestEvents_Update_StatusPlusFields_EmitsBothInOrder(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("combined")
	mustCreateTask(t, env.taskSvc, task)

	newTitle := "combined renamed"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		Status:  ptr("active"),
		Title:   &newTitle,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	events := listAllEvents(t, env.store)
	var statusIdx, modIdx = -1, -1
	for i, e := range events {
		switch e.Type {
		case domain.EventStatusChanged:
			statusIdx = i
		case domain.EventTaskModified:
			modIdx = i
		}
	}
	if statusIdx < 0 || modIdx < 0 {
		t.Fatalf("expected both status_changed and task_modified, got %v", countByType(events))
	}
	if statusIdx > modIdx {
		t.Fatalf("expected status_changed before task_modified, got status=%d modified=%d", statusIdx, modIdx)
	}
}

func TestEvents_Claim_EmitsTaskClaimedOnly(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	registerTestPlayer(t, env, "agent-1")
	ctx := WithActor(context.Background(), "agent-1")

	task := newMinimalTask("claim me")
	mustCreateTask(t, env.taskSvc, task)

	_, err := env.taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	events := listAllEvents(t, env.store)
	counts := countByType(events)
	if counts[domain.EventTaskClaimed] != 1 {
		t.Fatalf("expected 1 task_claimed, got %d", counts[domain.EventTaskClaimed])
	}
	if counts[domain.EventTaskModified] != 0 {
		t.Fatalf("expected 0 task_modified on Claim, got %d", counts[domain.EventTaskModified])
	}
	evt := firstEventOfType(t, events, domain.EventTaskClaimed)
	payload := evt.Payload.(domain.TaskClaimedPayload)
	if payload.ClaimedBy != "agent-1" {
		t.Fatalf("claimed_by: got %q, want agent-1", payload.ClaimedBy)
	}
}

func TestEvents_Release_EmitsTaskReleased(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	registerTestPlayer(t, env, "agent-1")
	ctx := WithActor(context.Background(), "agent-1")

	task := newMinimalTask("release me")
	mustCreateTask(t, env.taskSvc, task)
	claimed, err := env.taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := env.taskSvc.Release(ctx, claimed.ShortID, "agent-1", claimed.Version); err != nil {
		t.Fatalf("Release: %v", err)
	}

	events := listAllEvents(t, env.store)
	counts := countByType(events)
	if counts[domain.EventTaskReleased] != 1 {
		t.Fatalf("expected 1 task_released, got %d", counts[domain.EventTaskReleased])
	}
	evt := firstEventOfType(t, events, domain.EventTaskReleased)
	payload := evt.Payload.(domain.TaskReleasedPayload)
	if payload.ReleasedBy != "agent-1" {
		t.Fatalf("released_by: got %q, want agent-1", payload.ReleasedBy)
	}
}

func TestEvents_Complete_EmitsTaskCompletedOnly(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("complete me")
	mustCreateTask(t, env.taskSvc, task)
	started, err := env.taskSvc.Start(ctx, task.ShortID, task.Version, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Reset events from Create/Start so we only assert on Complete emissions.
	// List before and compare counts after instead.
	baseline := countByType(listAllEvents(t, env.store))

	if _, err := env.taskSvc.Complete(ctx, started.ShortID, started.Version); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	after := countByType(listAllEvents(t, env.store))
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	if delta(domain.EventTaskCompleted) != 1 {
		t.Fatalf("expected +1 task_completed, got +%d", delta(domain.EventTaskCompleted))
	}
	if delta(domain.EventStatusChanged) != 0 {
		t.Fatalf("expected +0 status_changed on Complete, got +%d", delta(domain.EventStatusChanged))
	}
	evt := firstEventOfType(t, listAllEvents(t, env.store), domain.EventTaskCompleted)
	payload := evt.Payload.(domain.TaskCompletedPayload)
	if payload.PrevStatus != "active" {
		t.Fatalf("prev_status: got %q, want active", payload.PrevStatus)
	}
}

func TestEvents_Delete_EmitsTaskDeletedOnly(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("delete me")
	mustCreateTask(t, env.taskSvc, task)
	baseline := countByType(listAllEvents(t, env.store))

	if _, err := env.taskSvc.Delete(ctx, task.ShortID, task.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	after := countByType(listAllEvents(t, env.store))
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	if delta(domain.EventTaskDeleted) != 1 {
		t.Fatalf("expected +1 task_deleted, got +%d", delta(domain.EventTaskDeleted))
	}
	if delta(domain.EventStatusChanged) != 0 {
		t.Fatalf("expected +0 status_changed on Delete, got +%d", delta(domain.EventStatusChanged))
	}
	evt := firstEventOfType(t, listAllEvents(t, env.store), domain.EventTaskDeleted)
	payload := evt.Payload.(domain.TaskDeletedPayload)
	if payload.PrevStatus != "pending" {
		t.Fatalf("prev_status: got %q, want pending", payload.PrevStatus)
	}
}

func TestEvents_AutoComplete_CascadeEmitsStatusChangedAutoComplete(t *testing.T) {
	t.Parallel()
	env := testTaskEnvWithSettings(t, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, err := env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")
	if err != nil {
		t.Fatalf("Start parent: %v", err)
	}

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, err = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")
	if err != nil {
		t.Fatalf("Start child: %v", err)
	}

	baseline := countByType(listAllEvents(t, env.store))
	if _, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version); err != nil {
		t.Fatalf("Complete child: %v", err)
	}

	events := listAllEvents(t, env.store)
	after := countByType(events)
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	if delta(domain.EventTaskCompleted) != 1 {
		t.Fatalf("expected +1 task_completed, got +%d", delta(domain.EventTaskCompleted))
	}

	// The cascade should produce exactly one status_changed(auto_complete) for
	// the parent.
	var auto int
	for _, e := range events {
		if e.Type != domain.EventStatusChanged {
			continue
		}
		if p, ok := e.Payload.(domain.StatusChangedPayload); ok && p.Source == "auto_complete" {
			auto++
			if e.EntityID != parent.ID.String() {
				t.Fatalf("auto_complete entity_id: got %q, want parent %q", e.EntityID, parent.ID.String())
			}
		}
	}
	if auto != 1 {
		t.Fatalf("expected 1 status_changed(auto_complete), got %d", auto)
	}
}

func TestEvents_AutoRevert_CascadeEmitsStatusChangedAutoRevert(t *testing.T) {
	t.Parallel()
	env := testTaskEnvWithSettings(t, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &domain.AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "pending",
		},
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")
	child, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	if err != nil {
		t.Fatalf("Complete child: %v", err)
	}

	baseline := countByType(listAllEvents(t, env.store))
	// Reopen: completed -> pending on child. Parent should revert.
	if _, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	}); err != nil {
		t.Fatalf("Update child: %v", err)
	}

	events := listAllEvents(t, env.store)
	after := countByType(events)
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	// Reopen emits child status_changed(user) + parent status_changed(auto_revert)
	if delta(domain.EventStatusChanged) != 2 {
		t.Fatalf("expected +2 status_changed (user + auto_revert), got +%d", delta(domain.EventStatusChanged))
	}
	var revert int
	for _, e := range events {
		if e.Type != domain.EventStatusChanged {
			continue
		}
		if p, ok := e.Payload.(domain.StatusChangedPayload); ok && p.Source == "auto_revert" {
			revert++
			if e.EntityID != parent.ID.String() {
				t.Fatalf("auto_revert entity_id: got %q, want parent %q", e.EntityID, parent.ID.String())
			}
		}
	}
	if revert != 1 {
		t.Fatalf("expected 1 status_changed(auto_revert), got %d", revert)
	}
}

func TestEvents_ActorPropagation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		actor string
	}{
		{name: "with_actor", actor: "german"},
		{name: "no_actor", actor: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := testTaskEnv(t)
			ctx := WithActor(context.Background(), tc.actor)
			task := newMinimalTask("actor test " + tc.name)
			if err := env.taskSvc.Create(ctx, task); err != nil {
				t.Fatalf("Create: %v", err)
			}
			evt := firstEventOfType(t, listAllEvents(t, env.store), domain.EventTaskCreated)
			if tc.actor == "" {
				if evt.PlayerID != nil {
					t.Fatalf("player_id: got %v, want nil", *evt.PlayerID)
				}
			} else {
				if evt.PlayerID == nil || *evt.PlayerID != tc.actor {
					t.Fatalf("player_id: got %v, want %q", evt.PlayerID, tc.actor)
				}
			}
		})
	}
}

func keysOf[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
