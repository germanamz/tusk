package domain

import "time"

const (
	EventRelationAdded   EventType = "relation_added"
	EventRelationRemoved EventType = "relation_removed"
)

type RelationAddedPayload struct {
	Kind          EventType `json:"kind"`
	SourceShortID string    `json:"source_short_id"`
	TargetShortID string    `json:"target_short_id"`
	RelationKind  string    `json:"relation_kind"` // "blocks" | "relates_to" | "duplicates"
}

func (RelationAddedPayload) EventKind() EventType { return EventRelationAdded }

type RelationRemovedPayload struct {
	Kind          EventType `json:"kind"`
	SourceShortID string    `json:"source_short_id"`
	TargetShortID string    `json:"target_short_id"`
	RelationKind  string    `json:"relation_kind"`
}

func (RelationRemovedPayload) EventKind() EventType { return EventRelationRemoved }

func NewRelationAddedEvent(rel *Relation, sourceShortID, targetShortID string, actor *string) *Event {
	payload := RelationAddedPayload{
		Kind:          EventRelationAdded,
		SourceShortID: sourceShortID,
		TargetShortID: targetShortID,
		RelationKind:  rel.RelationType,
	}
	return newRelationEvent(rel, EventRelationAdded, payload, actor)
}

func NewRelationRemovedEvent(rel *Relation, sourceShortID, targetShortID string, actor *string) *Event {
	payload := RelationRemovedPayload{
		Kind:          EventRelationRemoved,
		SourceShortID: sourceShortID,
		TargetShortID: targetShortID,
		RelationKind:  rel.RelationType,
	}
	return newRelationEvent(rel, EventRelationRemoved, payload, actor)
}

func newRelationEvent(rel *Relation, kind EventType, payload EventPayload, actor *string) *Event {
	return &Event{
		ID:         newEventID(),
		Type:       kind,
		EntityID:   rel.ID.String(),
		EntityKind: EntityRelation,
		PlayerID:   actor,
		Payload:    payload,
		CreatedAt:  time.Now().UTC().Truncate(time.Millisecond),
	}
}
