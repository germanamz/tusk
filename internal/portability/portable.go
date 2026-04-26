// Package portability owns the neutral wire representation of a tusk
// workspace plus the JSON encoder and decoder. It has no dependency on
// the service or repository layers — codec consumers (CLI commands,
// the PortabilityService) are responsible for translating between the
// PortableWorkspace value and the live workspace.
package portability

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SchemaVersion is the current portable wire-format version. Incremented
// on any breaking change to the PortableWorkspace shape (new required
// fields, removed fields, changed types, changed semantic meaning).
// Additive optional fields do not bump.
const SchemaVersion = 1

// PortableWorkspace is the root of an exported tusk workspace.
// Top-level lists are flat — entities reference each other by ID.
type PortableWorkspace struct {
	SchemaVersion int       `json:"schema_version"`
	TuskVersion   string    `json:"tusk_version"`
	ExportedAt    time.Time `json:"exported_at"`

	Workflows   []PortableWorkflow   `json:"workflows"`
	Projects    []PortableProject    `json:"projects"`
	Players     []PortablePlayer     `json:"players"`
	Tags        []PortableTag        `json:"tags"`
	Tasks       []PortableTask       `json:"tasks"`
	Relations   []PortableRelation   `json:"relations"`
	Annotations []PortableAnnotation `json:"annotations"`
	Notes       []PortableNote       `json:"notes"`
	Events      []PortableEvent      `json:"events"`
}

// PortableWorkflow mirrors domain.Workflow. Statuses map status name to
// the list of role strings; transitions are encoded as ordered
// from/to pairs.
type PortableWorkflow struct {
	ID          uuid.UUID                       `json:"id"`
	Name        string                          `json:"name"`
	Statuses    map[string]PortableStatusConfig `json:"statuses"`
	Transitions []PortableWorkflowTransition    `json:"transitions"`
	Version     int                             `json:"version"`
	CreatedAt   time.Time                       `json:"created_at"`
	UpdatedAt   time.Time                       `json:"updated_at"`
}

// PortableStatusConfig mirrors domain.StatusConfig. Roles is a list of
// role-name strings (domain.StatusRole is a string alias).
type PortableStatusConfig struct {
	Roles []string `json:"roles"`
}

// PortableWorkflowTransition mirrors domain.WorkflowTransition.
type PortableWorkflowTransition struct {
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
}

// PortableProject mirrors domain.Project.
type PortableProject struct {
	ID          uuid.UUID               `json:"id"`
	Name        string                  `json:"name"`
	WorkflowID  uuid.UUID               `json:"workflow_id"`
	Description string                  `json:"description"`
	Settings    PortableProjectSettings `json:"settings"`
	Version     int                     `json:"version"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

// PortableProjectSettings mirrors domain.ProjectSettings field-for-field.
type PortableProjectSettings struct {
	AutoCompleteParent *PortableAutoCompleteConfig `json:"auto_complete_parent,omitempty"`
	AutoRevertParent   *PortableAutoRevertConfig   `json:"auto_revert_parent,omitempty"`
	Urgency            *PortableUrgencyOverrides   `json:"urgency,omitempty"`
	NoteWindowSize     *int                        `json:"note_window_size,omitempty"`
	Taxonomy           *PortableTaxonomy           `json:"taxonomy,omitempty"`
}

// PortableAutoCompleteConfig mirrors domain.AutoCompleteConfig.
type PortableAutoCompleteConfig struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

// PortableAutoRevertConfig mirrors domain.AutoRevertConfig.
type PortableAutoRevertConfig struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

// PortableUrgencyOverrides mirrors domain.UrgencyOverrides field-for-field.
// Each weight is a *float64 with omitempty: nil fields inherit from the
// next layer in the resolution chain.
type PortableUrgencyOverrides struct {
	PriorityWeight    *float64 `json:"priority_weight,omitempty"`
	DueWeight         *float64 `json:"due_weight,omitempty"`
	AgeWeight         *float64 `json:"age_weight,omitempty"`
	ActiveWeight      *float64 `json:"active_weight,omitempty"`
	BlockingWeight    *float64 `json:"blocking_weight,omitempty"`
	BlockedWeight     *float64 `json:"blocked_weight,omitempty"`
	TagsWeight        *float64 `json:"tags_weight,omitempty"`
	ProjectWeight     *float64 `json:"project_weight,omitempty"`
	AnnotationsWeight *float64 `json:"annotations_weight,omitempty"`
	WaitingWeight     *float64 `json:"waiting_weight,omitempty"`
}

// PortableTaxonomy mirrors domain.Taxonomy. It is an ordered list of
// peer-name groups; index 0 is the top rank.
type PortableTaxonomy [][]string

// PortablePlayer mirrors domain.Player.
type PortablePlayer struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	NoteWindowSize *int      `json:"note_window_size,omitempty"`
	RegisteredAt   time.Time `json:"registered_at"`
	LastSeenAt     time.Time `json:"last_seen_at"`
}

// PortableTag mirrors domain.Tag. Color is nullable.
type PortableTag struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color *string   `json:"color,omitempty"`
}

// PortableTask mirrors the persisted fields of domain.Task. The transient
// computed fields (Urgency, EffectiveWeights) are intentionally excluded.
type PortableTask struct {
	ID               uuid.UUID                 `json:"id"`
	ShortID          string                    `json:"short_id"`
	ParentID         *uuid.UUID                `json:"parent_id,omitempty"`
	ProjectID        uuid.UUID                 `json:"project_id"`
	Title            string                    `json:"title"`
	Description      string                    `json:"description"`
	Level            *string                   `json:"level,omitempty"`
	Status           string                    `json:"status"`
	Priority         int                       `json:"priority"`
	Order            *float64                  `json:"order"`
	Version          int                       `json:"version"`
	DueAt            *time.Time                `json:"due_at,omitempty"`
	WaitUntil        *time.Time                `json:"wait_until,omitempty"`
	RecurrenceRule   *string                   `json:"recurrence_rule,omitempty"`
	Tags             []string                  `json:"tags"`
	UDA              map[string]string         `json:"uda"`
	UrgencyOverrides *PortableUrgencyOverrides `json:"urgency_overrides,omitempty"`
	ClaimedBy        *string                   `json:"claimed_by,omitempty"`
	ClaimedAt        *time.Time                `json:"claimed_at,omitempty"`
	CreatedAt        time.Time                 `json:"created_at"`
	ModifiedAt       time.Time                 `json:"modified_at"`
}

// PortableRelation mirrors domain.Relation.
type PortableRelation struct {
	ID           uuid.UUID `json:"id"`
	SourceID     uuid.UUID `json:"source_id"`
	TargetID     uuid.UUID `json:"target_id"`
	RelationType string    `json:"relation_type"`
	CreatedAt    time.Time `json:"created_at"`
}

// PortableAnnotation mirrors domain.Annotation.
type PortableAnnotation struct {
	ID        uuid.UUID `json:"id"`
	TaskID    uuid.UUID `json:"task_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// PortableNote mirrors domain.Note. TaskID and ArchivedAt are nullable.
// Metadata mirrors the domain map[string]any to preserve arbitrary
// values written by future MCP/CLI clients.
type PortableNote struct {
	ID         uuid.UUID      `json:"id"`
	ProjectID  uuid.UUID      `json:"project_id"`
	PlayerID   string         `json:"player_id"`
	TaskID     *uuid.UUID     `json:"task_id,omitempty"`
	Body       string         `json:"body"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	ArchivedAt *time.Time     `json:"archived_at,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

// PortableEvent mirrors domain.Event. Payload is kept as a json.RawMessage
// so unknown event kinds round-trip losslessly without the codec needing
// to know every payload schema.
type PortableEvent struct {
	ID         uuid.UUID       `json:"id"`
	Type       string          `json:"type"`
	EntityID   string          `json:"entity_id"`
	EntityKind string          `json:"entity_kind"`
	PlayerID   *string         `json:"player_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}
