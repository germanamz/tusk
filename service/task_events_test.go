package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/sqlite"
)

// listAllEvents returns every event recorded in the store. Tests assert on the
// subset of types they care about via countByType.
func listAllEvents(test *testing.T, store *sqlite.Store) []*domain.Event {
	test.Helper()
	repo := sqlite.NewEventRepo(store.DB(), 10000, 1000)
	events, err := repo.List(context.Background(), repository.EventFilter{})

	if err != nil {
		test.Fatalf("listing events: %v", err)
	}

	return events
}

// countByType groups events by Type so assertions do not depend on the total
// event count — adding a new event kind elsewhere should not fail these tests.
func countByType(events []*domain.Event) map[domain.EventType]int {
	counts := make(map[domain.EventType]int)
	for _, event := range events {
		counts[event.Type]++
	}
	return counts
}

// firstEventOfType returns the first event of the given type, or fails the test.
func firstEventOfType(test *testing.T, events []*domain.Event, typ domain.EventType) *domain.Event {
	test.Helper()
	for _, event := range events {
		if event.Type == typ {
			return event
		}
	}
	test.Fatalf("no event of type %q found among %d events", typ, len(events))
	return nil
}

// registerTestPlayer inserts a player row through the default bundle so Claim
// calls find a valid player record.
func registerTestPlayer(test *testing.T, env *testEnv, id string) {
	test.Helper()
	repo := sqlite.NewPlayerRepo(env.store.DB())
	svc := NewPlayerService(repo)

	if _, err := svc.Register(context.Background(), id, "agent"); err != nil {
		test.Fatalf("registering player %q: %v", id, err)
	}
}

func TestEvents_Create_EmitsTaskCreated(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)

	ctx := WithActor(context.Background(), "german")
	task := newMinimalTask("Write event tests")

	if err := env.taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	events := listAllEvents(test, env.store)
	counts := countByType(events)

	if counts[domain.EventTaskCreated] != 1 {
		test.Fatalf("expected 1 task_created, got %d (all=%v)", counts[domain.EventTaskCreated], counts)
	}

	event := firstEventOfType(test, events, domain.EventTaskCreated)

	if event.EntityID != task.ID.String() {
		test.Fatalf("entity_id: got %q, want %q", event.EntityID, task.ID.String())
	}

	if event.EntityKind != domain.EntityTask {
		test.Fatalf("entity_kind: got %q, want %q", event.EntityKind, domain.EntityTask)
	}

	if event.PlayerID == nil || *event.PlayerID != "german" {
		test.Fatalf("player_id: got %v, want *\"german\"", event.PlayerID)
	}

	payload, ok := event.Payload.(domain.TaskCreatedPayload)

	if !ok {
		test.Fatalf("payload: got %T, want TaskCreatedPayload", event.Payload)
	}

	if payload.Title != "Write event tests" {
		test.Fatalf("payload.title: got %q, want %q", payload.Title, "Write event tests")
	}
}

func TestEvents_Create_NoActor_PlayerIDNil(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)

	task := newMinimalTask("No actor")

	if err := env.taskSvc.Create(context.Background(), task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	event := firstEventOfType(test, listAllEvents(test, env.store), domain.EventTaskCreated)

	if event.PlayerID != nil {
		test.Fatalf("player_id: got %v, want nil", *event.PlayerID)
	}
}

func TestEvents_Update_OnlyNonStatusFields_EmitsTaskModified(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := WithActor(context.Background(), "german")

	task := newMinimalTask("priority test")
	mustCreateTask(test, env.taskSvc, task)

	newPri := 4
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  task.ShortID,
		Version:  task.Version,
		Priority: &newPri,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	events := listAllEvents(test, env.store)
	counts := countByType(events)

	if counts[domain.EventTaskModified] != 1 {
		test.Fatalf("expected 1 task_modified, got %d (all=%v)", counts[domain.EventTaskModified], counts)
	}

	if counts[domain.EventStatusChanged] != 0 {
		test.Fatalf("expected 0 status_changed, got %d", counts[domain.EventStatusChanged])
	}

	event := firstEventOfType(test, events, domain.EventTaskModified)
	payload, ok := event.Payload.(domain.TaskModifiedPayload)

	if !ok {
		test.Fatalf("payload: got %T, want TaskModifiedPayload", event.Payload)
	}

	if _, hasPri := payload.Changes["priority"]; !hasPri {
		test.Fatalf("changes should include 'priority', got keys=%v", keysOf(payload.Changes))
	}

	if _, hasStatus := payload.Changes["status"]; hasStatus {
		test.Fatalf("changes must not include 'status' — status flows via status_changed")
	}
}

func TestEvents_Update_OnlyStatus_EmitsStatusChangedUser(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("status test")
	mustCreateTask(test, env.taskSvc, task)

	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		Status:  ptr("active"),
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	events := listAllEvents(test, env.store)
	counts := countByType(events)

	if counts[domain.EventStatusChanged] != 1 {
		test.Fatalf("expected 1 status_changed, got %d", counts[domain.EventStatusChanged])
	}

	if counts[domain.EventTaskModified] != 0 {
		test.Fatalf("expected 0 task_modified, got %d", counts[domain.EventTaskModified])
	}

	event := firstEventOfType(test, events, domain.EventStatusChanged)
	payload, ok := event.Payload.(domain.StatusChangedPayload)

	if !ok {
		test.Fatalf("payload: got %T, want StatusChangedPayload", event.Payload)
	}

	if payload.Source != "user" {
		test.Fatalf("source: got %q, want \"user\"", payload.Source)
	}

	if payload.From != "pending" || payload.To != "active" {
		test.Fatalf("from/to: got %q → %q, want pending → active", payload.From, payload.To)
	}
}

func TestEvents_Update_StatusPlusFields_EmitsBothInOrder(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("combined")
	mustCreateTask(test, env.taskSvc, task)

	newTitle := "combined renamed"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		Status:  ptr("active"),
		Title:   &newTitle,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	events := listAllEvents(test, env.store)
	var statusIdx, modIdx = -1, -1

	for index, event := range events {
		switch event.Type {
		case domain.EventStatusChanged:
			statusIdx = index
		case domain.EventTaskModified:
			modIdx = index
		}
	}

	if statusIdx < 0 || modIdx < 0 {
		test.Fatalf("expected both status_changed and task_modified, got %v", countByType(events))
	}

	if statusIdx > modIdx {
		test.Fatalf("expected status_changed before task_modified, got status=%d modified=%d", statusIdx, modIdx)
	}
}

func TestEvents_Claim_EmitsTaskClaimedOnly(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	registerTestPlayer(test, env, "agent-1")
	ctx := WithActor(context.Background(), "agent-1")

	task := newMinimalTask("claim me")
	mustCreateTask(test, env.taskSvc, task)

	_, err := env.taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	if err != nil {
		test.Fatalf("Claim: %v", err)
	}

	events := listAllEvents(test, env.store)
	counts := countByType(events)

	if counts[domain.EventTaskClaimed] != 1 {
		test.Fatalf("expected 1 task_claimed, got %d", counts[domain.EventTaskClaimed])
	}

	if counts[domain.EventTaskModified] != 0 {
		test.Fatalf("expected 0 task_modified on Claim, got %d", counts[domain.EventTaskModified])
	}

	event := firstEventOfType(test, events, domain.EventTaskClaimed)
	payload := event.Payload.(domain.TaskClaimedPayload)

	if payload.ClaimedBy != "agent-1" {
		test.Fatalf("claimed_by: got %q, want agent-1", payload.ClaimedBy)
	}
}

func TestEvents_Release_EmitsTaskReleased(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	registerTestPlayer(test, env, "agent-1")
	ctx := WithActor(context.Background(), "agent-1")

	task := newMinimalTask("release me")
	mustCreateTask(test, env.taskSvc, task)

	claimed, err := env.taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	if err != nil {
		test.Fatalf("Claim: %v", err)
	}

	if _, releaseErr := env.taskSvc.Release(ctx, claimed.ShortID, "agent-1", claimed.Version); releaseErr != nil {
		test.Fatalf("Release: %v", releaseErr)
	}

	events := listAllEvents(test, env.store)
	counts := countByType(events)

	if counts[domain.EventTaskReleased] != 1 {
		test.Fatalf("expected 1 task_released, got %d", counts[domain.EventTaskReleased])
	}

	event := firstEventOfType(test, events, domain.EventTaskReleased)
	payload := event.Payload.(domain.TaskReleasedPayload)

	if payload.ReleasedBy != "agent-1" {
		test.Fatalf("released_by: got %q, want agent-1", payload.ReleasedBy)
	}
}

func TestEvents_Complete_EmitsTaskCompletedOnly(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("complete me")
	mustCreateTask(test, env.taskSvc, task)

	started, err := env.taskSvc.Start(ctx, task.ShortID, task.Version, "")

	if err != nil {
		test.Fatalf("Start: %v", err)
	}

	// Reset events from Create/Start so we only assert on Complete emissions.
	// List before and compare counts after instead.
	baseline := countByType(listAllEvents(test, env.store))

	if _, completeErr := env.taskSvc.Complete(ctx, started.ShortID, started.Version); completeErr != nil {
		test.Fatalf("Complete: %v", completeErr)
	}

	after := countByType(listAllEvents(test, env.store))
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskCompleted) != 1 {
		test.Fatalf("expected +1 task_completed, got +%d", delta(domain.EventTaskCompleted))
	}

	if delta(domain.EventStatusChanged) != 0 {
		test.Fatalf("expected +0 status_changed on Complete, got +%d", delta(domain.EventStatusChanged))
	}

	event := firstEventOfType(test, listAllEvents(test, env.store), domain.EventTaskCompleted)
	payload := event.Payload.(domain.TaskCompletedPayload)

	if payload.PrevStatus != "active" {
		test.Fatalf("prev_status: got %q, want active", payload.PrevStatus)
	}
}

func TestEvents_Delete_EmitsTaskDeletedOnly(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("delete me")
	mustCreateTask(test, env.taskSvc, task)
	baseline := countByType(listAllEvents(test, env.store))

	if _, deleteErr := env.taskSvc.Delete(ctx, task.ShortID, task.Version); deleteErr != nil {
		test.Fatalf("Delete: %v", deleteErr)
	}

	after := countByType(listAllEvents(test, env.store))
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskDeleted) != 1 {
		test.Fatalf("expected +1 task_deleted, got +%d", delta(domain.EventTaskDeleted))
	}

	if delta(domain.EventStatusChanged) != 0 {
		test.Fatalf("expected +0 status_changed on Delete, got +%d", delta(domain.EventStatusChanged))
	}

	event := firstEventOfType(test, listAllEvents(test, env.store), domain.EventTaskDeleted)
	payload := event.Payload.(domain.TaskDeletedPayload)

	if payload.PrevStatus != "pending" {
		test.Fatalf("prev_status: got %q, want pending", payload.PrevStatus)
	}
}

func TestEvents_AutoComplete_CascadeEmitsStatusChangedAutoComplete(test *testing.T) {
	test.Parallel()
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)

	parent, err := env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	if err != nil {
		test.Fatalf("Start parent: %v", err)
	}

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)

	child, err = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	if err != nil {
		test.Fatalf("Start child: %v", err)
	}

	baseline := countByType(listAllEvents(test, env.store))

	if _, completeErr := env.taskSvc.Complete(ctx, child.ShortID, child.Version); completeErr != nil {
		test.Fatalf("Complete child: %v", completeErr)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskCompleted) != 1 {
		test.Fatalf("expected +1 task_completed, got +%d", delta(domain.EventTaskCompleted))
	}

	// The cascade should produce exactly one status_changed(auto_complete) for
	// the parent.
	var auto int

	for _, event := range events {
		if event.Type != domain.EventStatusChanged {
			continue
		}

		if payload, ok := event.Payload.(domain.StatusChangedPayload); ok && payload.Source == "auto_complete" {
			auto++

			if event.EntityID != parent.ID.String() {
				test.Fatalf("auto_complete entity_id: got %q, want parent %q", event.EntityID, parent.ID.String())
			}
		}
	}

	if auto != 1 {
		test.Fatalf("expected 1 status_changed(auto_complete), got %d", auto)
	}
}

func TestEvents_AutoRevert_CascadeEmitsStatusChangedAutoRevert(test *testing.T) {
	test.Parallel()
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
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
	mustCreateTask(test, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	child, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version)

	if err != nil {
		test.Fatalf("Complete child: %v", err)
	}

	baseline := countByType(listAllEvents(test, env.store))

	// Reopen: completed -> pending on child. Parent should revert.
	if _, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	}); updateErr != nil {
		test.Fatalf("Update child: %v", updateErr)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	// Reopen emits child status_changed(user) + parent status_changed(auto_revert)
	if delta(domain.EventStatusChanged) != 2 {
		test.Fatalf("expected +2 status_changed (user + auto_revert), got +%d", delta(domain.EventStatusChanged))
	}

	var revert int

	for _, event := range events {
		if event.Type != domain.EventStatusChanged {
			continue
		}

		if payload, ok := event.Payload.(domain.StatusChangedPayload); ok && payload.Source == "auto_revert" {
			revert++

			if event.EntityID != parent.ID.String() {
				test.Fatalf("auto_revert entity_id: got %q, want parent %q", event.EntityID, parent.ID.String())
			}
		}
	}

	if revert != 1 {
		test.Fatalf("expected 1 status_changed(auto_revert), got %d", revert)
	}
}

func TestEvents_ActorPropagation(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name  string
		actor string
	}{
		{name: "with_actor", actor: "german"},
		{name: "no_actor", actor: ""},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			env := testTaskEnv(test)
			ctx := WithActor(context.Background(), testCase.actor)
			task := newMinimalTask("actor test " + testCase.name)

			if err := env.taskSvc.Create(ctx, task); err != nil {
				test.Fatalf("Create: %v", err)
			}

			event := firstEventOfType(test, listAllEvents(test, env.store), domain.EventTaskCreated)

			if testCase.actor == "" {
				if event.PlayerID != nil {
					test.Fatalf("player_id: got %v, want nil", *event.PlayerID)
				}
			} else {
				if event.PlayerID == nil || *event.PlayerID != testCase.actor {
					test.Fatalf("player_id: got %v, want %q", event.PlayerID, testCase.actor)
				}
			}
		})
	}
}

func TestEvents_Start_AutoClaim_EmitsTaskStartedOnly(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	registerTestPlayer(test, env, "agent-1")
	ctx := WithActor(context.Background(), "agent-1")

	task := newMinimalTask("auto-claim start")
	mustCreateTask(test, env.taskSvc, task)
	baseline := countByType(listAllEvents(test, env.store))

	started, err := env.taskSvc.Start(ctx, task.ShortID, task.Version, "agent-1")

	if err != nil {
		test.Fatalf("Start: %v", err)
	}

	if started.ClaimedBy == nil || *started.ClaimedBy != "agent-1" {
		test.Fatalf("ClaimedBy: got %v, want agent-1", started.ClaimedBy)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskStarted) != 1 {
		test.Fatalf("expected +1 task_started, got +%d", delta(domain.EventTaskStarted))
	}

	if delta(domain.EventStatusChanged) != 0 {
		test.Fatalf("expected 0 status_changed, got +%d", delta(domain.EventStatusChanged))
	}

	if delta(domain.EventTaskClaimed) != 0 {
		test.Fatalf("expected 0 task_claimed, got +%d", delta(domain.EventTaskClaimed))
	}

	if delta(domain.EventTaskModified) != 0 {
		test.Fatalf("expected 0 task_modified, got +%d", delta(domain.EventTaskModified))
	}

	event := firstEventOfType(test, events, domain.EventTaskStarted)
	payload := event.Payload.(domain.TaskStartedPayload)

	if !payload.AutoClaimed {
		test.Fatalf("auto_claimed: got false, want true")
	}

	if payload.PrevStatus != "pending" {
		test.Fatalf("prev_status: got %q, want pending", payload.PrevStatus)
	}
}

func TestEvents_Start_AlreadyClaimedSamePlayer_EmitsTaskStartedNotAutoClaimed(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	registerTestPlayer(test, env, "agent-1")
	ctx := WithActor(context.Background(), "agent-1")

	task := newMinimalTask("pre-claimed")
	mustCreateTask(test, env.taskSvc, task)

	claimed, err := env.taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	if err != nil {
		test.Fatalf("Claim: %v", err)
	}

	baseline := countByType(listAllEvents(test, env.store))

	started, startErr := env.taskSvc.Start(ctx, claimed.ShortID, claimed.Version, "agent-1")

	if startErr != nil {
		test.Fatalf("Start: %v", startErr)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskStarted) != 1 {
		test.Fatalf("expected +1 task_started, got +%d", delta(domain.EventTaskStarted))
	}

	if delta(domain.EventStatusChanged) != 0 || delta(domain.EventTaskClaimed) != 0 || delta(domain.EventTaskModified) != 0 {
		test.Fatalf("unexpected events: status=%d claimed=%d modified=%d",
			delta(domain.EventStatusChanged), delta(domain.EventTaskClaimed), delta(domain.EventTaskModified))
	}

	// Grab the most recent task_started (there may be others from older tests if
	// the pool grew, but in this test only one was created post-baseline).
	var startedEvent *domain.Event

	for _, event := range events {
		if event.Type == domain.EventTaskStarted && event.EntityID == started.ID.String() {
			startedEvent = event
		}
	}

	if startedEvent == nil {
		test.Fatalf("no task_started event for started task")
	}

	payload := startedEvent.Payload.(domain.TaskStartedPayload)

	if payload.AutoClaimed {
		test.Fatalf("auto_claimed: got true, want false (already claimed by same player)")
	}
}

func TestEvents_Start_NoPlayer_EmitsTaskStartedNotAutoClaimed(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := WithActor(context.Background(), "german")

	task := newMinimalTask("no player")
	mustCreateTask(test, env.taskSvc, task)
	baseline := countByType(listAllEvents(test, env.store))

	if _, startErr := env.taskSvc.Start(ctx, task.ShortID, task.Version, ""); startErr != nil {
		test.Fatalf("Start: %v", startErr)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskStarted) != 1 {
		test.Fatalf("expected +1 task_started, got +%d", delta(domain.EventTaskStarted))
	}

	if delta(domain.EventStatusChanged) != 0 || delta(domain.EventTaskClaimed) != 0 || delta(domain.EventTaskModified) != 0 {
		test.Fatalf("unexpected events: status=%d claimed=%d modified=%d",
			delta(domain.EventStatusChanged), delta(domain.EventTaskClaimed), delta(domain.EventTaskModified))
	}

	event := firstEventOfType(test, events, domain.EventTaskStarted)
	payload := event.Payload.(domain.TaskStartedPayload)

	if payload.AutoClaimed {
		test.Fatalf("auto_claimed: got true, want false (no player supplied)")
	}

	if event.PlayerID == nil || *event.PlayerID != "german" {
		test.Fatalf("player_id: got %v, want *\"german\" (from actor)", event.PlayerID)
	}
}

func TestEvents_Start_NoActor_PlayerIDNil(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)

	task := newMinimalTask("no actor start")
	mustCreateTask(test, env.taskSvc, task)
	baseline := countByType(listAllEvents(test, env.store))

	if _, startErr := env.taskSvc.Start(context.Background(), task.ShortID, task.Version, ""); startErr != nil {
		test.Fatalf("Start: %v", startErr)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskStarted) != 1 {
		test.Fatalf("expected +1 task_started, got +%d", delta(domain.EventTaskStarted))
	}

	for _, event := range events {
		if event.Type == domain.EventTaskStarted && event.EntityID == task.ID.String() {
			if event.PlayerID != nil {
				test.Fatalf("player_id: got %v, want nil (no actor)", *event.PlayerID)
			}

			return
		}
	}

	test.Fatalf("no task_started event for new task")
}

func TestEvents_Pop_EmitsTaskPoppedOnly(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	registerTestPlayer(test, env, "agent-1")
	ctx := WithActor(context.Background(), "agent-1")

	task := newMinimalTask("pop me")
	mustCreateTask(test, env.taskSvc, task)
	baseline := countByType(listAllEvents(test, env.store))

	popped, err := env.taskSvc.Pop(ctx, "agent-1", nil)

	if err != nil {
		test.Fatalf("Pop: %v", err)
	}

	if popped.ClaimedBy == nil || *popped.ClaimedBy != "agent-1" {
		test.Fatalf("ClaimedBy: got %v, want agent-1", popped.ClaimedBy)
	}

	if popped.Status != "active" {
		test.Fatalf("Status: got %q, want active", popped.Status)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskPopped) != 1 {
		test.Fatalf("expected +1 task_popped, got +%d", delta(domain.EventTaskPopped))
	}

	if delta(domain.EventTaskClaimed) != 0 {
		test.Fatalf("expected 0 task_claimed, got +%d", delta(domain.EventTaskClaimed))
	}

	if delta(domain.EventTaskStarted) != 0 {
		test.Fatalf("expected 0 task_started, got +%d", delta(domain.EventTaskStarted))
	}

	if delta(domain.EventStatusChanged) != 0 {
		test.Fatalf("expected 0 status_changed, got +%d", delta(domain.EventStatusChanged))
	}

	if delta(domain.EventTaskModified) != 0 {
		test.Fatalf("expected 0 task_modified, got +%d", delta(domain.EventTaskModified))
	}

	event := firstEventOfType(test, events, domain.EventTaskPopped)
	payload := event.Payload.(domain.TaskPoppedPayload)

	if payload.ClaimedBy != "agent-1" {
		test.Fatalf("claimed_by: got %q, want agent-1", payload.ClaimedBy)
	}

	if payload.PrevStatus != "pending" {
		test.Fatalf("prev_status: got %q, want pending", payload.PrevStatus)
	}
}

func TestEvents_Pop_NoAvailable_EmitsNoEvents(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	registerTestPlayer(test, env, "agent-1")
	ctx := WithActor(context.Background(), "agent-1")

	baseline := countByType(listAllEvents(test, env.store))

	_, err := env.taskSvc.Pop(ctx, "agent-1", nil)

	if !errors.Is(err, domain.ErrNoAvailableTasks) {
		test.Fatalf("Pop: got %v, want ErrNoAvailableTasks", err)
	}

	after := countByType(listAllEvents(test, env.store))

	for eventType, count := range after {
		if count != baseline[eventType] {
			test.Fatalf("event count for %s changed: baseline=%d after=%d", eventType, baseline[eventType], count)
		}
	}
}

func TestEvents_Pop_Race_EmitsOnePerWinner(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	registerTestPlayer(test, env, "agent-1")
	registerTestPlayer(test, env, "agent-2")

	task1 := newMinimalTask("race-1")
	mustCreateTask(test, env.taskSvc, task1)
	task2 := newMinimalTask("race-2")
	mustCreateTask(test, env.taskSvc, task2)

	baseline := countByType(listAllEvents(test, env.store))

	var wg sync.WaitGroup
	results := make([]*domain.Task, 2)
	errs := make([]error, 2)
	wg.Add(2)

	go func() {
		defer wg.Done()
		ctx := WithActor(context.Background(), "agent-1")
		results[0], errs[0] = env.taskSvc.Pop(ctx, "agent-1", nil)
	}()

	go func() {
		defer wg.Done()
		ctx := WithActor(context.Background(), "agent-2")
		results[1], errs[1] = env.taskSvc.Pop(ctx, "agent-2", nil)
	}()

	wg.Wait()

	for index, err := range errs {
		if err != nil {
			test.Fatalf("Pop %d: %v", index, err)
		}
	}

	if results[0].ID == results[1].ID {
		test.Fatalf("both goroutines popped same task %s", results[0].ID)
	}

	if *results[0].ClaimedBy == *results[1].ClaimedBy {
		test.Fatalf("both popped by same player %q", *results[0].ClaimedBy)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(eventType domain.EventType) int { return after[eventType] - baseline[eventType] }

	if delta(domain.EventTaskPopped) != 2 {
		test.Fatalf("expected +2 task_popped, got +%d", delta(domain.EventTaskPopped))
	}

	if delta(domain.EventTaskClaimed) != 0 || delta(domain.EventTaskStarted) != 0 || delta(domain.EventStatusChanged) != 0 {
		test.Fatalf("unexpected noise: claimed=%d started=%d status=%d",
			delta(domain.EventTaskClaimed), delta(domain.EventTaskStarted), delta(domain.EventStatusChanged))
	}
}

func keysOf[Value any](m map[string]Value) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
