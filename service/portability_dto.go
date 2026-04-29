// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/portability"
	"github.com/google/uuid"
)

// workflowToPortable copies every persisted workflow field into the wire
// shape. Status roles are stringified so the wire format does not couple
// to domain.StatusRole.
func workflowToPortable(workflow *domain.Workflow) portability.PortableWorkflow {
	statuses := make(map[string]portability.PortableStatusConfig, len(workflow.Statuses))
	for name, sc := range workflow.Statuses {
		roles := make([]string, len(sc.Roles))
		for i, role := range sc.Roles {
			roles[i] = string(role)
		}
		statuses[name] = portability.PortableStatusConfig{Roles: roles}
	}
	transitions := make([]portability.PortableWorkflowTransition, len(workflow.Transitions))
	for i, transition := range workflow.Transitions {
		transitions[i] = portability.PortableWorkflowTransition{
			FromStatus: transition.FromStatus,
			ToStatus:   transition.ToStatus,
		}
	}
	return portability.PortableWorkflow{
		ID:          workflow.ID,
		Name:        workflow.Name,
		Statuses:    statuses,
		Transitions: transitions,
		Version:     workflow.Version,
		CreatedAt:   workflow.CreatedAt,
		UpdatedAt:   workflow.UpdatedAt,
	}
}

// workflowFromPortable inverts workflowToPortable. Status role strings are
// passed through unchanged — domain.ValidateWorkflow rejects unknown
// roles, so the validation pass surfaces malformed dumps.
func workflowFromPortable(portable portability.PortableWorkflow) *domain.Workflow {
	statuses := make(map[string]domain.StatusConfig, len(portable.Statuses))
	for name, sc := range portable.Statuses {
		roles := make([]domain.StatusRole, len(sc.Roles))
		for i, role := range sc.Roles {
			roles[i] = domain.StatusRole(role)
		}
		statuses[name] = domain.StatusConfig{Roles: roles}
	}
	transitions := make([]domain.WorkflowTransition, len(portable.Transitions))
	for i, transition := range portable.Transitions {
		transitions[i] = domain.WorkflowTransition{
			FromStatus: transition.FromStatus,
			ToStatus:   transition.ToStatus,
		}
	}
	return &domain.Workflow{
		ID:          portable.ID,
		Name:        portable.Name,
		Statuses:    statuses,
		Transitions: transitions,
		Version:     portable.Version,
		CreatedAt:   portable.CreatedAt,
		UpdatedAt:   portable.UpdatedAt,
	}
}

// projectToPortable copies every persisted project field. Settings nested
// pointer fields are shallow-cloned so the dump is safe to mutate without
// corrupting the live workspace.
func projectToPortable(project *domain.Project) portability.PortableProject {
	return portability.PortableProject{
		ID:          project.ID,
		Name:        project.Name,
		WorkflowID:  project.WorkflowID,
		Description: project.Description,
		Settings:    projectSettingsToPortable(project.Settings),
		Version:     project.Version,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}
}

func projectFromPortable(portable portability.PortableProject) *domain.Project {
	return &domain.Project{
		ID:          portable.ID,
		Name:        portable.Name,
		WorkflowID:  portable.WorkflowID,
		Description: portable.Description,
		Settings:    projectSettingsFromPortable(portable.Settings),
		Version:     portable.Version,
		CreatedAt:   portable.CreatedAt,
		UpdatedAt:   portable.UpdatedAt,
	}
}

func projectSettingsToPortable(settings domain.ProjectSettings) portability.PortableProjectSettings {
	out := portability.PortableProjectSettings{
		NoteWindowSize: copyIntPtr(settings.NoteWindowSize),
	}
	if settings.AutoCompleteParent != nil {
		out.AutoCompleteParent = &portability.PortableAutoCompleteConfig{
			TriggerStatus: settings.AutoCompleteParent.TriggerStatus,
			TargetStatus:  settings.AutoCompleteParent.TargetStatus,
		}
	}
	if settings.AutoRevertParent != nil {
		out.AutoRevertParent = &portability.PortableAutoRevertConfig{
			TriggerStatus: settings.AutoRevertParent.TriggerStatus,
			TargetStatus:  settings.AutoRevertParent.TargetStatus,
		}
	}
	if settings.Urgency != nil {
		out.Urgency = urgencyToPortable(settings.Urgency)
	}
	if settings.Taxonomy != nil {
		clone := settings.Taxonomy.Clone()
		tax := portability.PortableTaxonomy(clone)
		out.Taxonomy = &tax
	}
	return out
}

func projectSettingsFromPortable(settings portability.PortableProjectSettings) domain.ProjectSettings {
	out := domain.ProjectSettings{
		NoteWindowSize: copyIntPtr(settings.NoteWindowSize),
	}
	if settings.AutoCompleteParent != nil {
		out.AutoCompleteParent = &domain.AutoCompleteConfig{
			TriggerStatus: settings.AutoCompleteParent.TriggerStatus,
			TargetStatus:  settings.AutoCompleteParent.TargetStatus,
		}
	}
	if settings.AutoRevertParent != nil {
		out.AutoRevertParent = &domain.AutoRevertConfig{
			TriggerStatus: settings.AutoRevertParent.TriggerStatus,
			TargetStatus:  settings.AutoRevertParent.TargetStatus,
		}
	}
	if settings.Urgency != nil {
		out.Urgency = urgencyFromPortable(settings.Urgency)
	}
	if settings.Taxonomy != nil {
		tax := domain.Taxonomy(*settings.Taxonomy).Clone()
		out.Taxonomy = &tax
	}
	return out
}

func urgencyToPortable(urgency *domain.UrgencyOverrides) *portability.PortableUrgencyOverrides {
	if urgency == nil {
		return nil
	}
	return &portability.PortableUrgencyOverrides{
		PriorityWeight:    copyFloatPtr(urgency.PriorityWeight),
		DueWeight:         copyFloatPtr(urgency.DueWeight),
		AgeWeight:         copyFloatPtr(urgency.AgeWeight),
		ActiveWeight:      copyFloatPtr(urgency.ActiveWeight),
		BlockingWeight:    copyFloatPtr(urgency.BlockingWeight),
		BlockedWeight:     copyFloatPtr(urgency.BlockedWeight),
		TagsWeight:        copyFloatPtr(urgency.TagsWeight),
		ProjectWeight:     copyFloatPtr(urgency.ProjectWeight),
		AnnotationsWeight: copyFloatPtr(urgency.AnnotationsWeight),
		WaitingWeight:     copyFloatPtr(urgency.WaitingWeight),
	}
}

func urgencyFromPortable(urgency *portability.PortableUrgencyOverrides) *domain.UrgencyOverrides {
	if urgency == nil {
		return nil
	}
	return &domain.UrgencyOverrides{
		PriorityWeight:    copyFloatPtr(urgency.PriorityWeight),
		DueWeight:         copyFloatPtr(urgency.DueWeight),
		AgeWeight:         copyFloatPtr(urgency.AgeWeight),
		ActiveWeight:      copyFloatPtr(urgency.ActiveWeight),
		BlockingWeight:    copyFloatPtr(urgency.BlockingWeight),
		BlockedWeight:     copyFloatPtr(urgency.BlockedWeight),
		TagsWeight:        copyFloatPtr(urgency.TagsWeight),
		ProjectWeight:     copyFloatPtr(urgency.ProjectWeight),
		AnnotationsWeight: copyFloatPtr(urgency.AnnotationsWeight),
		WaitingWeight:     copyFloatPtr(urgency.WaitingWeight),
	}
}

func playerToPortable(player *domain.Player) portability.PortablePlayer {
	return portability.PortablePlayer{
		ID:             player.ID,
		Type:           player.Type,
		NoteWindowSize: copyIntPtr(player.NoteWindowSize),
		RegisteredAt:   player.RegisteredAt,
		LastSeenAt:     player.LastSeenAt,
	}
}

func playerFromPortable(portable portability.PortablePlayer) *domain.Player {
	return &domain.Player{
		ID:             portable.ID,
		Type:           portable.Type,
		NoteWindowSize: copyIntPtr(portable.NoteWindowSize),
		RegisteredAt:   portable.RegisteredAt,
		LastSeenAt:     portable.LastSeenAt,
	}
}

func tagToPortable(tag *domain.Tag) portability.PortableTag {
	return portability.PortableTag{
		ID:    tag.ID,
		Name:  tag.Name,
		Color: copyStringPtr(tag.Color),
	}
}

func tagFromPortable(portable portability.PortableTag) *domain.Tag {
	return &domain.Tag{
		ID:    portable.ID,
		Name:  portable.Name,
		Color: copyStringPtr(portable.Color),
	}
}

// taskToPortable converts a domain task plus its associated tags into the
// wire shape. UDA values are constrained to strings by domain.ValidateUDA;
// any non-string value is dropped so the dump still encodes — exporting a
// domain-invalid task should not block a backup.
func taskToPortable(task *domain.Task, tags []*domain.Tag) portability.PortableTask {
	tagNames := make([]string, len(tags))
	for i, tag := range tags {
		tagNames[i] = tag.Name
	}
	uda := make(map[string]string, len(task.UDA))
	for k, v := range task.UDA {
		if str, ok := v.(string); ok {
			uda[k] = str
		}
	}
	return portability.PortableTask{
		ID:               task.ID,
		ShortID:          task.ShortID,
		ParentID:         copyUUIDPtr(task.ParentID),
		ProjectID:        task.ProjectID,
		Title:            task.Title,
		Description:      task.Description,
		Level:            copyStringPtr(task.Level),
		Status:           task.Status,
		Priority:         task.Priority,
		Order:            copyFloatPtr(task.Order),
		Version:          task.Version,
		DueAt:            copyTimePtr(task.DueAt),
		WaitUntil:        copyTimePtr(task.WaitUntil),
		RecurrenceRule:   copyStringPtr(task.RecurrenceRule),
		Tags:             tagNames,
		UDA:              uda,
		UrgencyOverrides: urgencyToPortable(task.UrgencyOverrides),
		ClaimedBy:        copyStringPtr(task.ClaimedBy),
		ClaimedAt:        copyTimePtr(task.ClaimedAt),
		CreatedAt:        task.CreatedAt,
		ModifiedAt:       task.ModifiedAt,
	}
}

// taskFromPortable returns the persisted task fields. The PortableTask
// Tags field is intentionally not copied — the join-table writes happen
// in a separate step so the apply pass can reorder them after the parent
// task has been inserted.
func taskFromPortable(portable portability.PortableTask) *domain.Task {
	uda := make(map[string]any, len(portable.UDA))
	for k, v := range portable.UDA {
		uda[k] = v
	}
	return &domain.Task{
		ID:               portable.ID,
		ShortID:          portable.ShortID,
		ParentID:         copyUUIDPtr(portable.ParentID),
		ProjectID:        portable.ProjectID,
		Title:            portable.Title,
		Description:      portable.Description,
		Level:            copyStringPtr(portable.Level),
		Status:           portable.Status,
		Priority:         portable.Priority,
		Order:            copyFloatPtr(portable.Order),
		Version:          portable.Version,
		DueAt:            copyTimePtr(portable.DueAt),
		WaitUntil:        copyTimePtr(portable.WaitUntil),
		RecurrenceRule:   copyStringPtr(portable.RecurrenceRule),
		UDA:              uda,
		UrgencyOverrides: urgencyFromPortable(portable.UrgencyOverrides),
		ClaimedBy:        copyStringPtr(portable.ClaimedBy),
		ClaimedAt:        copyTimePtr(portable.ClaimedAt),
		CreatedAt:        portable.CreatedAt,
		ModifiedAt:       portable.ModifiedAt,
	}
}

func relationToPortable(relation *domain.Relation) portability.PortableRelation {
	return portability.PortableRelation{
		ID:           relation.ID,
		SourceID:     relation.SourceID,
		TargetID:     relation.TargetID,
		RelationType: relation.RelationType,
		CreatedAt:    relation.CreatedAt,
	}
}

func relationFromPortable(portable portability.PortableRelation) *domain.Relation {
	return &domain.Relation{
		ID:           portable.ID,
		SourceID:     portable.SourceID,
		TargetID:     portable.TargetID,
		RelationType: portable.RelationType,
		CreatedAt:    portable.CreatedAt,
	}
}

func annotationToPortable(annotation *domain.Annotation) portability.PortableAnnotation {
	return portability.PortableAnnotation{
		ID:        annotation.ID,
		TaskID:    annotation.TaskID,
		Body:      annotation.Body,
		CreatedAt: annotation.CreatedAt,
	}
}

func annotationFromPortable(portable portability.PortableAnnotation) *domain.Annotation {
	return &domain.Annotation{
		ID:        portable.ID,
		TaskID:    portable.TaskID,
		Body:      portable.Body,
		CreatedAt: portable.CreatedAt,
	}
}

func noteToPortable(note *domain.Note) portability.PortableNote {
	var meta map[string]any
	if len(note.Metadata) > 0 {
		meta = make(map[string]any, len(note.Metadata))
		for k, v := range note.Metadata {
			meta[k] = v
		}
	}
	return portability.PortableNote{
		ID:         note.ID,
		ProjectID:  note.ProjectID,
		PlayerID:   note.PlayerID,
		TaskID:     copyUUIDPtr(note.TaskID),
		Body:       note.Body,
		Metadata:   meta,
		ArchivedAt: copyTimePtr(note.ArchivedAt),
		CreatedAt:  note.CreatedAt,
	}
}

func noteFromPortable(portable portability.PortableNote) *domain.Note {
	var meta map[string]any
	if len(portable.Metadata) > 0 {
		meta = make(map[string]any, len(portable.Metadata))
		for k, v := range portable.Metadata {
			meta[k] = v
		}
	}
	return &domain.Note{
		ID:         portable.ID,
		ProjectID:  portable.ProjectID,
		PlayerID:   portable.PlayerID,
		TaskID:     copyUUIDPtr(portable.TaskID),
		Body:       portable.Body,
		Metadata:   meta,
		ArchivedAt: copyTimePtr(portable.ArchivedAt),
		CreatedAt:  portable.CreatedAt,
	}
}

// eventToPortable serializes the event payload to a json.RawMessage so
// every payload kind — including UnknownPayload from future event types —
// round-trips losslessly. UnknownPayload's Raw field is tagged json:"-",
// so a direct Marshal would drop the bytes; serialize Raw instead.
func eventToPortable(event *domain.Event) (portability.PortableEvent, error) {
	var raw json.RawMessage
	if event.Payload != nil {
		var (
			rawBytes   []byte
			marshalErr error
		)
		if up, ok := event.Payload.(domain.UnknownPayload); ok {
			rawBytes, marshalErr = json.Marshal(up.Raw)
		} else {
			rawBytes, marshalErr = json.Marshal(event.Payload)
		}

		if marshalErr != nil {
			return portability.PortableEvent{}, fmt.Errorf("marshaling event %s payload: %w", event.ID, marshalErr)
		}

		raw = rawBytes
	}
	return portability.PortableEvent{
		ID:         event.ID,
		Type:       string(event.Type),
		EntityID:   event.EntityID,
		EntityKind: string(event.EntityKind),
		PlayerID:   copyStringPtr(event.PlayerID),
		Payload:    raw,
		CreatedAt:  event.CreatedAt,
	}, nil
}

// eventFromPortable rehydrates a domain.Event from the wire shape. The
// payload is wrapped in domain.UnknownPayload because the codec stores
// payload as opaque JSON; the EventRepo's Record path normalizes the
// stored bytes regardless of payload kind.
func eventFromPortable(portable portability.PortableEvent) (*domain.Event, error) {
	var raw map[string]any
	if len(portable.Payload) > 0 {
		if unmarshalErr := json.Unmarshal(portable.Payload, &raw); unmarshalErr != nil {
			return nil, fmt.Errorf("decoding event %s payload: %w", portable.ID, unmarshalErr)
		}
	}
	return &domain.Event{
		ID:         portable.ID,
		Type:       domain.EventType(portable.Type),
		EntityID:   portable.EntityID,
		EntityKind: domain.EntityKind(portable.EntityKind),
		PlayerID:   copyStringPtr(portable.PlayerID),
		Payload: domain.UnknownPayload{
			Kind: domain.EventType(portable.Type),
			Raw:  raw,
		},
		CreatedAt: portable.CreatedAt,
	}, nil
}

func copyStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func copyIntPtr(n *int) *int {
	if n == nil {
		return nil
	}
	v := *n
	return &v
}

func copyFloatPtr(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := *f
	return &v
}

func copyUUIDPtr(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	v := *id
	return &v
}

func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}
