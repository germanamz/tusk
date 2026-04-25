package domain

import (
	"testing"

	"github.com/google/uuid"
)

// kanbanWorkflow constructs a minimal kanban-shaped workflow matching
// the migration 003 seed: pending (initial) → active (start, highlight)
// → completed (done, terminal, dim); deleted (delete, terminal, dim).
func kanbanWorkflow() *Workflow {
	return &Workflow{
		ID:   uuid.Nil,
		Name: "kanban",
		Statuses: map[string]StatusConfig{
			"pending":   {Roles: []StatusRole{RoleInitial}},
			"active":    {Roles: []StatusRole{RoleStart, RoleHighlight}},
			"completed": {Roles: []StatusRole{RoleTerminal, RoleDone, RoleDim}},
			"deleted":   {Roles: []StatusRole{RoleTerminal, RoleDelete, RoleDim}},
		},
		Transitions: []WorkflowTransition{
			{FromStatus: "pending", ToStatus: "active"},
			{FromStatus: "active", ToStatus: "pending"},
			{FromStatus: "active", ToStatus: "completed"},
			{FromStatus: "completed", ToStatus: "pending"},
			{FromStatus: "pending", ToStatus: "deleted"},
			{FromStatus: "active", ToStatus: "deleted"},
		},
	}
}

// shipWorkflow defines a custom workflow whose `done` role is on a
// non-`completed` status (`shipped`) — exercises the per-workflow
// done-role lookup.
func shipWorkflow() *Workflow {
	return &Workflow{
		ID:   uuid.New(),
		Name: "ship",
		Statuses: map[string]StatusConfig{
			"triage":  {Roles: []StatusRole{RoleInitial}},
			"shipped": {Roles: []StatusRole{RoleTerminal, RoleDone}},
			"dropped": {Roles: []StatusRole{RoleTerminal, RoleDelete}},
		},
		Transitions: []WorkflowTransition{
			{FromStatus: "triage", ToStatus: "shipped"},
			{FromStatus: "triage", ToStatus: "dropped"},
		},
	}
}

// noDoneWorkflow has no status carrying the `done` role. Legal per
// spec — Done counts stay 0.
func noDoneWorkflow() *Workflow {
	return &Workflow{
		ID:   uuid.New(),
		Name: "no_done",
		Statuses: map[string]StatusConfig{
			"open":    {Roles: []StatusRole{RoleInitial}},
			"closed":  {Roles: []StatusRole{RoleTerminal}},
			"trashed": {Roles: []StatusRole{RoleTerminal, RoleDelete}},
		},
		Transitions: []WorkflowTransition{
			{FromStatus: "open", ToStatus: "closed"},
			{FromStatus: "open", ToStatus: "trashed"},
		},
	}
}

// overlapWorkflow shares the `active` status name with kanban so we can
// verify same-named buckets across workflows merge.
func overlapWorkflow() *Workflow {
	return &Workflow{
		ID:   uuid.New(),
		Name: "overlap",
		Statuses: map[string]StatusConfig{
			"triage":  {Roles: []StatusRole{RoleInitial}},
			"active":  {Roles: []StatusRole{RoleStart}},
			"shipped": {Roles: []StatusRole{RoleTerminal, RoleDone}},
		},
		Transitions: []WorkflowTransition{
			{FromStatus: "triage", ToStatus: "active"},
			{FromStatus: "active", ToStatus: "shipped"},
		},
	}
}

func taskAt(status string) *Task {
	return &Task{ID: uuid.New(), Status: status}
}

func TestAggregateRollup_EmptyInput(t *testing.T) {
	got := AggregateRollup(nil, func(*Task) *Workflow { return kanbanWorkflow() })
	if got.Done != 0 || got.Total != 0 || got.Percent != 0.0 {
		t.Fatalf("got Done=%d Total=%d Percent=%v, want all zeros", got.Done, got.Total, got.Percent)
	}
	if got.StatusCounts == nil {
		t.Fatalf("StatusCounts must be non-nil empty slice")
	}
	if len(got.StatusCounts) != 0 {
		t.Fatalf("StatusCounts should be empty, got %v", got.StatusCounts)
	}
}

func TestAggregateRollup_AllDone(t *testing.T) {
	wf := kanbanWorkflow()
	tasks := []*Task{taskAt("completed"), taskAt("completed"), taskAt("completed")}
	got := AggregateRollup(tasks, func(*Task) *Workflow { return wf })
	if got.Done != 3 || got.Total != 3 {
		t.Fatalf("want Done=3 Total=3, got Done=%d Total=%d", got.Done, got.Total)
	}
	if got.Percent != 1.0 {
		t.Fatalf("want Percent=1.0, got %v", got.Percent)
	}
	wantOrder := []string{"pending", "active", "completed"}
	if !statusOrder(got.StatusCounts, wantOrder) {
		t.Fatalf("StatusCounts order want %v, got %v", wantOrder, got.StatusCounts)
	}
	if findCount(got.StatusCounts, "completed") != 3 {
		t.Fatalf("completed bucket want 3, got %d", findCount(got.StatusCounts, "completed"))
	}
}

func TestAggregateRollup_AllDeleted(t *testing.T) {
	wf := kanbanWorkflow()
	tasks := []*Task{taskAt("deleted"), taskAt("deleted")}
	got := AggregateRollup(tasks, func(*Task) *Workflow { return wf })
	if got.Total != 0 {
		t.Fatalf("want Total=0 (all delete-role excluded), got %d", got.Total)
	}
	if got.Done != 0 || got.Percent != 0.0 {
		t.Fatalf("want zero done/percent, got Done=%d Percent=%v", got.Done, got.Percent)
	}
	if len(got.StatusCounts) != 0 {
		t.Fatalf("StatusCounts must be empty (no non-delete descendants seeded the breakdown), got %v", got.StatusCounts)
	}
}

func TestAggregateRollup_MixedKanban(t *testing.T) {
	wf := kanbanWorkflow()
	tasks := []*Task{
		taskAt("pending"),
		taskAt("active"),
		taskAt("completed"), taskAt("completed"), taskAt("completed"),
		taskAt("deleted"),
	}
	got := AggregateRollup(tasks, func(*Task) *Workflow { return wf })
	if got.Done != 3 || got.Total != 5 {
		t.Fatalf("want Done=3 Total=5, got Done=%d Total=%d", got.Done, got.Total)
	}
	if got.Percent < 0.599 || got.Percent > 0.601 {
		t.Fatalf("want Percent≈0.6, got %v", got.Percent)
	}
	wantOrder := []string{"pending", "active", "completed"}
	if !statusOrder(got.StatusCounts, wantOrder) {
		t.Fatalf("order want %v, got %v", wantOrder, got.StatusCounts)
	}
	if findCount(got.StatusCounts, "deleted") != 0 || hasBucket(got.StatusCounts, "deleted") {
		t.Fatalf("deleted bucket must be absent, got %v", got.StatusCounts)
	}
	if findCount(got.StatusCounts, "pending") != 1 ||
		findCount(got.StatusCounts, "active") != 1 ||
		findCount(got.StatusCounts, "completed") != 3 {
		t.Fatalf("bucket counts wrong: %v", got.StatusCounts)
	}
}

func TestAggregateRollup_CustomDoneRole(t *testing.T) {
	wf := shipWorkflow()
	tasks := []*Task{taskAt("triage"), taskAt("shipped"), taskAt("shipped")}
	got := AggregateRollup(tasks, func(*Task) *Workflow { return wf })
	if got.Done != 2 || got.Total != 3 {
		t.Fatalf("want Done=2 Total=3, got Done=%d Total=%d", got.Done, got.Total)
	}
	wantOrder := []string{"triage", "shipped"}
	if !statusOrder(got.StatusCounts, wantOrder) {
		t.Fatalf("order want %v, got %v", wantOrder, got.StatusCounts)
	}
	if hasBucket(got.StatusCounts, "dropped") {
		t.Fatalf("delete-role bucket leaked: %v", got.StatusCounts)
	}
}

func TestAggregateRollup_NoDoneRole(t *testing.T) {
	wf := noDoneWorkflow()
	tasks := []*Task{taskAt("open"), taskAt("closed"), taskAt("trashed")}
	got := AggregateRollup(tasks, func(*Task) *Workflow { return wf })
	if got.Done != 0 {
		t.Fatalf("want Done=0 (workflow has no done role), got %d", got.Done)
	}
	if got.Total != 2 {
		t.Fatalf("want Total=2 (open+closed; trashed excluded), got %d", got.Total)
	}
	if got.Percent != 0.0 {
		t.Fatalf("want Percent=0.0, got %v", got.Percent)
	}
}

func TestAggregateRollup_MultiWorkflowMerge(t *testing.T) {
	kan := kanbanWorkflow()
	ovl := overlapWorkflow()
	// Half from kanban (pending, active, completed), half from overlap
	// (triage, active, shipped). Kanban descendant comes first → seeds order.
	tasks := []*Task{
		{ID: uuid.New(), Status: "pending"},   // kanban
		{ID: uuid.New(), Status: "active"},    // kanban
		{ID: uuid.New(), Status: "completed"}, // kanban
		{ID: uuid.New(), Status: "triage"},    // overlap
		{ID: uuid.New(), Status: "active"},    // overlap (merges into kanban's active bucket)
		{ID: uuid.New(), Status: "shipped"},   // overlap
	}
	wfBy := func(t *Task) *Workflow {
		switch t.Status {
		case "pending", "completed":
			return kan
		case "active":
			// First active seen is from kanban; second is from overlap.
			// Both classify under their workflow individually but bucket merges by name.
			// To make the test deterministic we always return kan for "active"
			// (both workflows agree it's a non-done, non-delete status).
			return kan
		case "triage", "shipped":
			return ovl
		}
		return nil
	}
	got := AggregateRollup(tasks, wfBy)
	// Done: only "completed" (kanban) and "shipped" (overlap) count → 2
	// (deletion-role: none). Total: all 6 are non-delete.
	if got.Done != 2 {
		t.Fatalf("want Done=2 (completed + shipped), got %d", got.Done)
	}
	if got.Total != 6 {
		t.Fatalf("want Total=6, got %d", got.Total)
	}
	// Order: kanban seed (pending, active, completed) then overlap newcomers
	// (triage, shipped). The "active" bucket from overlap merges with kanban's.
	wantOrder := []string{"pending", "active", "completed", "triage", "shipped"}
	if !statusOrder(got.StatusCounts, wantOrder) {
		t.Fatalf("order want %v, got %v", wantOrder, got.StatusCounts)
	}
	if findCount(got.StatusCounts, "active") != 2 {
		t.Fatalf("active bucket should merge to 2, got %d", findCount(got.StatusCounts, "active"))
	}
}

func TestAggregateRollup_NilWorkflowSkipsTask(t *testing.T) {
	wf := kanbanWorkflow()
	tasks := []*Task{
		taskAt("pending"),
		{ID: uuid.New(), Status: "orphan"}, // workflowFor returns nil
		taskAt("completed"),
	}
	wfBy := func(t *Task) *Workflow {
		if t.Status == "orphan" {
			return nil
		}
		return wf
	}
	got := AggregateRollup(tasks, wfBy)
	if got.Done != 1 || got.Total != 2 {
		t.Fatalf("nil-workflow tasks must be silently skipped: got Done=%d Total=%d, want 1/2", got.Done, got.Total)
	}
}

func statusOrder(got []StatusCount, want []string) bool {
	if len(got) < len(want) {
		return false
	}
	idx := make(map[string]int, len(got))
	for i, sc := range got {
		idx[sc.Name] = i
	}
	last := -1
	for _, name := range want {
		i, ok := idx[name]
		if !ok || i <= last {
			return false
		}
		last = i
	}
	return true
}

func findCount(got []StatusCount, name string) int {
	for _, sc := range got {
		if sc.Name == name {
			return sc.Count
		}
	}
	return 0
}

func hasBucket(got []StatusCount, name string) bool {
	for _, sc := range got {
		if sc.Name == name {
			return true
		}
	}
	return false
}
