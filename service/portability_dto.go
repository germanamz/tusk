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
func workflowToPortable(w *domain.Workflow) portability.PortableWorkflow {
	statuses := make(map[string]portability.PortableStatusConfig, len(w.Statuses))
	for name, sc := range w.Statuses {
		roles := make([]string, len(sc.Roles))
		for i, r := range sc.Roles {
			roles[i] = string(r)
		}
		statuses[name] = portability.PortableStatusConfig{Roles: roles}
	}
	transitions := make([]portability.PortableWorkflowTransition, len(w.Transitions))
	for i, t := range w.Transitions {
		transitions[i] = portability.PortableWorkflowTransition{
			FromStatus: t.FromStatus,
			ToStatus:   t.ToStatus,
		}
	}
	return portability.PortableWorkflow{
		ID:          w.ID,
		Name:        w.Name,
		Statuses:    statuses,
		Transitions: transitions,
		Version:     w.Version,
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
	}
}

// workflowFromPortable inverts workflowToPortable. Status role strings are
// passed through unchanged — domain.ValidateWorkflow rejects unknown
// roles, so the validation pass surfaces malformed dumps.
func workflowFromPortable(p portability.PortableWorkflow) *domain.Workflow {
	statuses := make(map[string]domain.StatusConfig, len(p.Statuses))
	for name, sc := range p.Statuses {
		roles := make([]domain.StatusRole, len(sc.Roles))
		for i, r := range sc.Roles {
			roles[i] = domain.StatusRole(r)
		}
		statuses[name] = domain.StatusConfig{Roles: roles}
	}
	transitions := make([]domain.WorkflowTransition, len(p.Transitions))
	for i, t := range p.Transitions {
		transitions[i] = domain.WorkflowTransition{
			FromStatus: t.FromStatus,
			ToStatus:   t.ToStatus,
		}
	}
	return &domain.Workflow{
		ID:          p.ID,
		Name:        p.Name,
		Statuses:    statuses,
		Transitions: transitions,
		Version:     p.Version,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// projectToPortable copies every persisted project field. Settings nested
// pointer fields are shallow-cloned so the dump is safe to mutate without
// corrupting the live workspace.
func projectToPortable(p *domain.Project) portability.PortableProject {
	return portability.PortableProject{
		ID:         p.ID,
		Name:       p.Name,
		WorkflowID: p.WorkflowID,
		Settings:   projectSettingsToPortable(p.Settings),
		Version:    p.Version,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

func projectFromPortable(p portability.PortableProject) *domain.Project {
	return &domain.Project{
		ID:         p.ID,
		Name:       p.Name,
		WorkflowID: p.WorkflowID,
		Settings:   projectSettingsFromPortable(p.Settings),
		Version:    p.Version,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

func projectSettingsToPortable(s domain.ProjectSettings) portability.PortableProjectSettings {
	out := portability.PortableProjectSettings{
		NoteWindowSize: copyIntPtr(s.NoteWindowSize),
	}
	if s.AutoCompleteParent != nil {
		out.AutoCompleteParent = &portability.PortableAutoCompleteConfig{
			TriggerStatus: s.AutoCompleteParent.TriggerStatus,
			TargetStatus:  s.AutoCompleteParent.TargetStatus,
		}
	}
	if s.AutoRevertParent != nil {
		out.AutoRevertParent = &portability.PortableAutoRevertConfig{
			TriggerStatus: s.AutoRevertParent.TriggerStatus,
			TargetStatus:  s.AutoRevertParent.TargetStatus,
		}
	}
	if s.Urgency != nil {
		out.Urgency = urgencyToPortable(s.Urgency)
	}
	if s.Taxonomy != nil {
		clone := s.Taxonomy.Clone()
		tax := portability.PortableTaxonomy(clone)
		out.Taxonomy = &tax
	}
	return out
}

func projectSettingsFromPortable(s portability.PortableProjectSettings) domain.ProjectSettings {
	out := domain.ProjectSettings{
		NoteWindowSize: copyIntPtr(s.NoteWindowSize),
	}
	if s.AutoCompleteParent != nil {
		out.AutoCompleteParent = &domain.AutoCompleteConfig{
			TriggerStatus: s.AutoCompleteParent.TriggerStatus,
			TargetStatus:  s.AutoCompleteParent.TargetStatus,
		}
	}
	if s.AutoRevertParent != nil {
		out.AutoRevertParent = &domain.AutoRevertConfig{
			TriggerStatus: s.AutoRevertParent.TriggerStatus,
			TargetStatus:  s.AutoRevertParent.TargetStatus,
		}
	}
	if s.Urgency != nil {
		out.Urgency = urgencyFromPortable(s.Urgency)
	}
	if s.Taxonomy != nil {
		tax := domain.Taxonomy(*s.Taxonomy).Clone()
		out.Taxonomy = &tax
	}
	return out
}

func urgencyToPortable(u *domain.UrgencyOverrides) *portability.PortableUrgencyOverrides {
	if u == nil {
		return nil
	}
	return &portability.PortableUrgencyOverrides{
		PriorityWeight:    copyFloatPtr(u.PriorityWeight),
		DueWeight:         copyFloatPtr(u.DueWeight),
		AgeWeight:         copyFloatPtr(u.AgeWeight),
		ActiveWeight:      copyFloatPtr(u.ActiveWeight),
		BlockingWeight:    copyFloatPtr(u.BlockingWeight),
		BlockedWeight:     copyFloatPtr(u.BlockedWeight),
		TagsWeight:        copyFloatPtr(u.TagsWeight),
		ProjectWeight:     copyFloatPtr(u.ProjectWeight),
		AnnotationsWeight: copyFloatPtr(u.AnnotationsWeight),
		WaitingWeight:     copyFloatPtr(u.WaitingWeight),
	}
}

func urgencyFromPortable(u *portability.PortableUrgencyOverrides) *domain.UrgencyOverrides {
	if u == nil {
		return nil
	}
	return &domain.UrgencyOverrides{
		PriorityWeight:    copyFloatPtr(u.PriorityWeight),
		DueWeight:         copyFloatPtr(u.DueWeight),
		AgeWeight:         copyFloatPtr(u.AgeWeight),
		ActiveWeight:      copyFloatPtr(u.ActiveWeight),
		BlockingWeight:    copyFloatPtr(u.BlockingWeight),
		BlockedWeight:     copyFloatPtr(u.BlockedWeight),
		TagsWeight:        copyFloatPtr(u.TagsWeight),
		ProjectWeight:     copyFloatPtr(u.ProjectWeight),
		AnnotationsWeight: copyFloatPtr(u.AnnotationsWeight),
		WaitingWeight:     copyFloatPtr(u.WaitingWeight),
	}
}

func playerToPortable(p *domain.Player) portability.PortablePlayer {
	return portability.PortablePlayer{
		ID:             p.ID,
		Type:           p.Type,
		NoteWindowSize: copyIntPtr(p.NoteWindowSize),
		RegisteredAt:   p.RegisteredAt,
		LastSeenAt:     p.LastSeenAt,
	}
}

func playerFromPortable(p portability.PortablePlayer) *domain.Player {
	return &domain.Player{
		ID:             p.ID,
		Type:           p.Type,
		NoteWindowSize: copyIntPtr(p.NoteWindowSize),
		RegisteredAt:   p.RegisteredAt,
		LastSeenAt:     p.LastSeenAt,
	}
}

func tagToPortable(t *domain.Tag) portability.PortableTag {
	return portability.PortableTag{
		ID:    t.ID,
		Name:  t.Name,
		Color: copyStringPtr(t.Color),
	}
}

func tagFromPortable(t portability.PortableTag) *domain.Tag {
	return &domain.Tag{
		ID:    t.ID,
		Name:  t.Name,
		Color: copyStringPtr(t.Color),
	}
}

// taskToPortable converts a domain task plus its associated tags into the
// wire shape. UDA values are constrained to strings by domain.ValidateUDA;
// any non-string value is dropped so the dump still encodes — exporting a
// domain-invalid task should not block a backup.
func taskToPortable(t *domain.Task, tags []*domain.Tag) portability.PortableTask {
	tagNames := make([]string, len(tags))
	for i, tg := range tags {
		tagNames[i] = tg.Name
	}
	uda := make(map[string]string, len(t.UDA))
	for k, v := range t.UDA {
		if s, ok := v.(string); ok {
			uda[k] = s
		}
	}
	return portability.PortableTask{
		ID:               t.ID,
		ShortID:          t.ShortID,
		ParentID:         copyUUIDPtr(t.ParentID),
		ProjectID:        t.ProjectID,
		Title:            t.Title,
		Description:      t.Description,
		Level:            copyStringPtr(t.Level),
		Status:           t.Status,
		Priority:         t.Priority,
		Order:            copyFloatPtr(t.Order),
		Version:          t.Version,
		DueAt:            copyTimePtr(t.DueAt),
		WaitUntil:        copyTimePtr(t.WaitUntil),
		RecurrenceRule:   copyStringPtr(t.RecurrenceRule),
		Tags:             tagNames,
		UDA:              uda,
		UrgencyOverrides: urgencyToPortable(t.UrgencyOverrides),
		ClaimedBy:        copyStringPtr(t.ClaimedBy),
		ClaimedAt:        copyTimePtr(t.ClaimedAt),
		CreatedAt:        t.CreatedAt,
		ModifiedAt:       t.ModifiedAt,
	}
}

// taskFromPortable returns the persisted task fields. The PortableTask
// Tags field is intentionally not copied — the join-table writes happen
// in a separate step so the apply pass can reorder them after the parent
// task has been inserted.
func taskFromPortable(p portability.PortableTask) *domain.Task {
	uda := make(map[string]any, len(p.UDA))
	for k, v := range p.UDA {
		uda[k] = v
	}
	return &domain.Task{
		ID:               p.ID,
		ShortID:          p.ShortID,
		ParentID:         copyUUIDPtr(p.ParentID),
		ProjectID:        p.ProjectID,
		Title:            p.Title,
		Description:      p.Description,
		Level:            copyStringPtr(p.Level),
		Status:           p.Status,
		Priority:         p.Priority,
		Order:            copyFloatPtr(p.Order),
		Version:          p.Version,
		DueAt:            copyTimePtr(p.DueAt),
		WaitUntil:        copyTimePtr(p.WaitUntil),
		RecurrenceRule:   copyStringPtr(p.RecurrenceRule),
		UDA:              uda,
		UrgencyOverrides: urgencyFromPortable(p.UrgencyOverrides),
		ClaimedBy:        copyStringPtr(p.ClaimedBy),
		ClaimedAt:        copyTimePtr(p.ClaimedAt),
		CreatedAt:        p.CreatedAt,
		ModifiedAt:       p.ModifiedAt,
	}
}

func relationToPortable(r *domain.Relation) portability.PortableRelation {
	return portability.PortableRelation{
		ID:           r.ID,
		SourceID:     r.SourceID,
		TargetID:     r.TargetID,
		RelationType: r.RelationType,
		CreatedAt:    r.CreatedAt,
	}
}

func relationFromPortable(p portability.PortableRelation) *domain.Relation {
	return &domain.Relation{
		ID:           p.ID,
		SourceID:     p.SourceID,
		TargetID:     p.TargetID,
		RelationType: p.RelationType,
		CreatedAt:    p.CreatedAt,
	}
}

func annotationToPortable(a *domain.Annotation) portability.PortableAnnotation {
	return portability.PortableAnnotation{
		ID:        a.ID,
		TaskID:    a.TaskID,
		Body:      a.Body,
		CreatedAt: a.CreatedAt,
	}
}

func annotationFromPortable(p portability.PortableAnnotation) *domain.Annotation {
	return &domain.Annotation{
		ID:        p.ID,
		TaskID:    p.TaskID,
		Body:      p.Body,
		CreatedAt: p.CreatedAt,
	}
}

func noteToPortable(n *domain.Note) portability.PortableNote {
	var meta map[string]any
	if len(n.Metadata) > 0 {
		meta = make(map[string]any, len(n.Metadata))
		for k, v := range n.Metadata {
			meta[k] = v
		}
	}
	return portability.PortableNote{
		ID:         n.ID,
		ProjectID:  n.ProjectID,
		PlayerID:   n.PlayerID,
		TaskID:     copyUUIDPtr(n.TaskID),
		Body:       n.Body,
		Metadata:   meta,
		ArchivedAt: copyTimePtr(n.ArchivedAt),
		CreatedAt:  n.CreatedAt,
	}
}

func noteFromPortable(p portability.PortableNote) *domain.Note {
	var meta map[string]any
	if len(p.Metadata) > 0 {
		meta = make(map[string]any, len(p.Metadata))
		for k, v := range p.Metadata {
			meta[k] = v
		}
	}
	return &domain.Note{
		ID:         p.ID,
		ProjectID:  p.ProjectID,
		PlayerID:   p.PlayerID,
		TaskID:     copyUUIDPtr(p.TaskID),
		Body:       p.Body,
		Metadata:   meta,
		ArchivedAt: copyTimePtr(p.ArchivedAt),
		CreatedAt:  p.CreatedAt,
	}
}

// eventToPortable serializes the event payload to a json.RawMessage so
// every payload kind — including UnknownPayload from future event types —
// round-trips losslessly. UnknownPayload's Raw field is tagged json:"-",
// so a direct Marshal would drop the bytes; serialize Raw instead.
func eventToPortable(e *domain.Event) (portability.PortableEvent, error) {
	var raw json.RawMessage
	if e.Payload != nil {
		var (
			bytes []byte
			err   error
		)
		if up, ok := e.Payload.(domain.UnknownPayload); ok {
			bytes, err = json.Marshal(up.Raw)
		} else {
			bytes, err = json.Marshal(e.Payload)
		}
		if err != nil {
			return portability.PortableEvent{}, fmt.Errorf("marshaling event %s payload: %w", e.ID, err)
		}
		raw = bytes
	}
	return portability.PortableEvent{
		ID:         e.ID,
		Type:       string(e.Type),
		EntityID:   e.EntityID,
		EntityKind: string(e.EntityKind),
		PlayerID:   copyStringPtr(e.PlayerID),
		Payload:    raw,
		CreatedAt:  e.CreatedAt,
	}, nil
}

// eventFromPortable rehydrates a domain.Event from the wire shape. The
// payload is wrapped in domain.UnknownPayload because the codec stores
// payload as opaque JSON; the EventRepo's Record path normalizes the
// stored bytes regardless of payload kind.
func eventFromPortable(p portability.PortableEvent) (*domain.Event, error) {
	var raw map[string]any
	if len(p.Payload) > 0 {
		if err := json.Unmarshal(p.Payload, &raw); err != nil {
			return nil, fmt.Errorf("decoding event %s payload: %w", p.ID, err)
		}
	}
	return &domain.Event{
		ID:         p.ID,
		Type:       domain.EventType(p.Type),
		EntityID:   p.EntityID,
		EntityKind: domain.EntityKind(p.EntityKind),
		PlayerID:   copyStringPtr(p.PlayerID),
		Payload: domain.UnknownPayload{
			Kind: domain.EventType(p.Type),
			Raw:  raw,
		},
		CreatedAt: p.CreatedAt,
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
