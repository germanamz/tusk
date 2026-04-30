package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	EventTaskCreated   EventType = "task_created"
	EventTaskModified  EventType = "task_modified"
	EventStatusChanged EventType = "status_changed"
	EventTaskStarted   EventType = "task_started"
	EventTaskClaimed   EventType = "task_claimed"
	EventTaskReleased  EventType = "task_released"
	EventTaskCompleted EventType = "task_completed"
	EventTaskDeleted   EventType = "task_deleted"
	EventTaskPopped    EventType = "task_popped"
	EventTaskMoved     EventType = "task_moved"
)

type TaskCreatedPayload struct {
	Kind      EventType `json:"kind"`
	ShortID   string    `json:"short_id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  int       `json:"priority"`
	ProjectID string    `json:"project_id"`
	ParentID  *string   `json:"parent_id,omitempty"`
	Order     *float64  `json:"order,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
}

func (TaskCreatedPayload) EventKind() EventType { return EventTaskCreated }

type FieldChange struct {
	From any `json:"from"`
	To   any `json:"to"`
}

type TaskModifiedPayload struct {
	Kind    EventType              `json:"kind"`
	ShortID string                 `json:"short_id"`
	Changes map[string]FieldChange `json:"changes"`
}

func (TaskModifiedPayload) EventKind() EventType { return EventTaskModified }

type StatusChangedPayload struct {
	Kind    EventType `json:"kind"`
	ShortID string    `json:"short_id"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	ToRoles []string  `json:"to_roles"`
	Source  string    `json:"source"` // "user" | "auto_complete" | "auto_revert"
}

func (StatusChangedPayload) EventKind() EventType { return EventStatusChanged }

type TaskStartedPayload struct {
	Kind        EventType `json:"kind"`
	ShortID     string    `json:"short_id"`
	PrevStatus  string    `json:"prev_status"`
	AutoClaimed bool      `json:"auto_claimed"`
}

func (TaskStartedPayload) EventKind() EventType { return EventTaskStarted }

type TaskClaimedPayload struct {
	Kind      EventType `json:"kind"`
	ShortID   string    `json:"short_id"`
	ClaimedBy string    `json:"claimed_by"`
}

func (TaskClaimedPayload) EventKind() EventType { return EventTaskClaimed }

type TaskReleasedPayload struct {
	Kind       EventType `json:"kind"`
	ShortID    string    `json:"short_id"`
	ReleasedBy string    `json:"released_by"`
}

func (TaskReleasedPayload) EventKind() EventType { return EventTaskReleased }

type TaskCompletedPayload struct {
	Kind       EventType `json:"kind"`
	ShortID    string    `json:"short_id"`
	PrevStatus string    `json:"prev_status"`
}

func (TaskCompletedPayload) EventKind() EventType { return EventTaskCompleted }

type TaskDeletedPayload struct {
	Kind       EventType `json:"kind"`
	ShortID    string    `json:"short_id"`
	PrevStatus string    `json:"prev_status"`
}

func (TaskDeletedPayload) EventKind() EventType { return EventTaskDeleted }

type TaskPoppedPayload struct {
	Kind       EventType `json:"kind"`
	ShortID    string    `json:"short_id"`
	ClaimedBy  string    `json:"claimed_by"`
	PrevStatus string    `json:"prev_status"`
}

func (TaskPoppedPayload) EventKind() EventType { return EventTaskPopped }

type TaskMovedPayload struct {
	Kind        EventType  `json:"kind"`
	ShortID     string     `json:"short_id"`
	OldParentID *uuid.UUID `json:"old_parent_id,omitempty"`
	NewParentID *uuid.UUID `json:"new_parent_id,omitempty"`
	OldOrder    *float64   `json:"old_order,omitempty"`
	NewOrder    *float64   `json:"new_order,omitempty"`
}

func (TaskMovedPayload) EventKind() EventType { return EventTaskMoved }

// newTaskEvent builds a generic *Event for a task-scoped payload. All task
// event constructors funnel through this helper so ID generation, timestamp
// truncation, and entity wiring stay consistent.
func newTaskEvent(task *Task, kind EventType, payload EventPayload, actor *string) *Event {
	return &Event{
		ID:         newEventID(),
		Type:       kind,
		EntityID:   task.ID.String(),
		EntityKind: EntityTask,
		PlayerID:   actor,
		Payload:    payload,
		CreatedAt:  time.Now().UTC().Truncate(time.Millisecond),
	}
}

// newEventID returns a fresh UUIDv7 when the pinned uuid library supports it,
// falling back to UUIDv4 if not. The created_at index orders events, so ID
// ordering is not load-bearing.
func newEventID() uuid.UUID {
	id, err := uuid.NewV7()

	if err != nil {
		return uuid.New()
	}

	return id
}

func NewTaskCreatedEvent(task *Task, actor *string) *Event {
	var parentID *string
	if task.ParentID != nil {
		str := task.ParentID.String()
		parentID = &str
	}
	payload := TaskCreatedPayload{
		Kind:      EventTaskCreated,
		ShortID:   task.ShortID,
		Title:     task.Title,
		Status:    task.Status,
		Priority:  task.Priority,
		ProjectID: task.ProjectID.String(),
		ParentID:  parentID,
		Order:     task.Order,
	}
	return newTaskEvent(task, EventTaskCreated, payload, actor)
}

func NewTaskModifiedEvent(task *Task, changes map[string]FieldChange, actor *string) *Event {
	payload := TaskModifiedPayload{
		Kind:    EventTaskModified,
		ShortID: task.ShortID,
		Changes: changes,
	}
	return newTaskEvent(task, EventTaskModified, payload, actor)
}

func NewStatusChangedEvent(task *Task, from, to string, toRoles []string, source string, actor *string) *Event {
	payload := StatusChangedPayload{
		Kind:    EventStatusChanged,
		ShortID: task.ShortID,
		From:    from,
		To:      to,
		ToRoles: toRoles,
		Source:  source,
	}
	return newTaskEvent(task, EventStatusChanged, payload, actor)
}

func NewTaskStartedEvent(task *Task, prevStatus string, autoClaimed bool, actor *string) *Event {
	payload := TaskStartedPayload{
		Kind:        EventTaskStarted,
		ShortID:     task.ShortID,
		PrevStatus:  prevStatus,
		AutoClaimed: autoClaimed,
	}
	return newTaskEvent(task, EventTaskStarted, payload, actor)
}

func NewTaskClaimedEvent(task *Task, claimedBy string, actor *string) *Event {
	payload := TaskClaimedPayload{
		Kind:      EventTaskClaimed,
		ShortID:   task.ShortID,
		ClaimedBy: claimedBy,
	}
	return newTaskEvent(task, EventTaskClaimed, payload, actor)
}

func NewTaskReleasedEvent(task *Task, releasedBy string, actor *string) *Event {
	payload := TaskReleasedPayload{
		Kind:       EventTaskReleased,
		ShortID:    task.ShortID,
		ReleasedBy: releasedBy,
	}
	return newTaskEvent(task, EventTaskReleased, payload, actor)
}

func NewTaskCompletedEvent(task *Task, prevStatus string, actor *string) *Event {
	payload := TaskCompletedPayload{
		Kind:       EventTaskCompleted,
		ShortID:    task.ShortID,
		PrevStatus: prevStatus,
	}
	return newTaskEvent(task, EventTaskCompleted, payload, actor)
}

func NewTaskDeletedEvent(task *Task, prevStatus string, actor *string) *Event {
	payload := TaskDeletedPayload{
		Kind:       EventTaskDeleted,
		ShortID:    task.ShortID,
		PrevStatus: prevStatus,
	}
	return newTaskEvent(task, EventTaskDeleted, payload, actor)
}

func NewTaskPoppedEvent(task *Task, claimedBy, prevStatus string, actor *string) *Event {
	payload := TaskPoppedPayload{
		Kind:       EventTaskPopped,
		ShortID:    task.ShortID,
		ClaimedBy:  claimedBy,
		PrevStatus: prevStatus,
	}
	return newTaskEvent(task, EventTaskPopped, payload, actor)
}

func NewTaskMovedEvent(task *Task, oldParent, newParent *uuid.UUID, oldOrder, newOrder *float64, actor *string) *Event {
	payload := TaskMovedPayload{
		Kind:        EventTaskMoved,
		ShortID:     task.ShortID,
		OldParentID: oldParent,
		NewParentID: newParent,
		OldOrder:    oldOrder,
		NewOrder:    newOrder,
	}
	return newTaskEvent(task, EventTaskMoved, payload, actor)
}
