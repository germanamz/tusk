package portability

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// normalizePayloads rewrites every event Payload to a compact form so
// pretty-printed round-trips compare cleanly with reflect.DeepEqual.
func normalizePayloads(ws *PortableWorkspace) {
	for i := range ws.Events {
		raw := ws.Events[i].Payload
		if len(raw) == 0 {
			continue
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err == nil {
			ws.Events[i].Payload = buf.Bytes()
		}
	}
}

// buildFullWorkspace returns a PortableWorkspace populated with one of
// every entity kind, every field set to a non-zero value, used by the
// happy-path round-trip test.
func buildFullWorkspace() *PortableWorkspace {
	wfID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	projID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	tagID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	taskID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	parentID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	relID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	annID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	noteID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	evID := uuid.MustParse("99999999-9999-9999-9999-999999999999")

	exportedAt := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 4, 25, 15, 30, 0, 0, time.UTC)
	dueAt := time.Date(2026, 5, 1, 17, 0, 0, 0, time.UTC)
	waitUntil := time.Date(2026, 4, 28, 8, 0, 0, 0, time.UTC)
	claimedAt := time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
	archivedAt := time.Date(2026, 4, 26, 11, 0, 0, 0, time.UTC)

	return &PortableWorkspace{
		SchemaVersion: SchemaVersion,
		TuskVersion:   "v0.13.0",
		ExportedAt:    exportedAt,
		Workflows: []PortableWorkflow{{
			ID:   wfID,
			Name: "kanban",
			Statuses: map[string]PortableStatusConfig{
				"pending":   {Roles: []string{"initial"}},
				"active":    {Roles: []string{"start"}},
				"completed": {Roles: []string{"terminal", "done"}},
			},
			Transitions: []PortableWorkflowTransition{
				{FromStatus: "pending", ToStatus: "active"},
				{FromStatus: "active", ToStatus: "completed"},
			},
			Version:   3,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}},
		Projects: []PortableProject{{
			ID:         projID,
			Name:       "alpha",
			WorkflowID: wfID,
			Settings: PortableProjectSettings{
				AutoCompleteParent: &PortableAutoCompleteConfig{TriggerStatus: "completed", TargetStatus: "completed"},
				AutoRevertParent:   &PortableAutoRevertConfig{TriggerStatus: "active", TargetStatus: "active"},
				Urgency: &PortableUrgencyOverrides{
					PriorityWeight: new(2.5),
					DueWeight:      new(1.0),
				},
				NoteWindowSize: new(50),
				Taxonomy:       &PortableTaxonomy{{"epic"}, {"story", "spike"}, {"task"}},
			},
			Version:   2,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}},
		Players: []PortablePlayer{{
			ID:             "alice",
			Type:           "human",
			NoteWindowSize: new(25),
			RegisteredAt:   createdAt,
			LastSeenAt:     updatedAt,
		}},
		Tags: []PortableTag{{
			ID:    tagID,
			Name:  "urgent",
			Color: new("#ff0000"),
		}},
		Tasks: []PortableTask{{
			ID:             taskID,
			ShortID:        "abcd1234",
			ParentID:       new(parentID),
			ProjectID:      projID,
			Title:          "Round-trip me",
			Description:    "Line one\nLine two\nwith <html> & special characters",
			Level:          new("task"),
			Status:         "active",
			Priority:       3,
			Order:          new(1.5),
			Version:        7,
			DueAt:          new(dueAt),
			WaitUntil:      new(waitUntil),
			RecurrenceRule: new("FREQ=DAILY"),
			Tags:           []string{"urgent", "backend"},
			UDA:            map[string]string{"effort": "5", "owner": "alice"},
			UrgencyOverrides: &PortableUrgencyOverrides{
				PriorityWeight: new(0.75),
				ActiveWeight:   new(2.0),
			},
			ClaimedBy:  new("alice"),
			ClaimedAt:  new(claimedAt),
			CreatedAt:  createdAt,
			ModifiedAt: updatedAt,
		}},
		Relations: []PortableRelation{{
			ID:           relID,
			SourceID:     taskID,
			TargetID:     parentID,
			RelationType: "blocks",
			CreatedAt:    createdAt,
		}},
		Annotations: []PortableAnnotation{{
			ID:        annID,
			TaskID:    taskID,
			Body:      "noted",
			CreatedAt: createdAt,
		}},
		Notes: []PortableNote{{
			ID:         noteID,
			ProjectID:  projID,
			PlayerID:   "alice",
			TaskID:     new(taskID),
			Body:       "thinking out loud",
			Metadata:   map[string]any{"topic": "auth"},
			ArchivedAt: new(archivedAt),
			CreatedAt:  createdAt,
		}},
		Events: []PortableEvent{{
			ID:         evID,
			Type:       "task_created",
			EntityID:   taskID.String(),
			EntityKind: "task",
			PlayerID:   new("alice"),
			Payload:    json.RawMessage(`{"kind":"task_created","short_id":"abcd1234"}`),
			CreatedAt:  createdAt,
		}},
	}
}

func TestEncodeDecode_RoundTrip_HappyPath(t *testing.T) {
	want := buildFullWorkspace()

	var buf bytes.Buffer
	if err := Encode(&buf, want); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	normalizePayloads(want)
	normalizePayloads(got)

	if !reflect.DeepEqual(want, got) {
		t.Errorf("round-trip mismatch\n want=%#v\n got=%#v", want, got)
	}
}

func TestEncode_OrderNullEmittedExplicitly(t *testing.T) {
	// v0.13 follow-up at ROADMAP.md:1305 — cleared task.order must
	// serialize as JSON null, never omitted, so import can distinguish
	// "no change" from "clear to default".
	ws := &PortableWorkspace{
		SchemaVersion: SchemaVersion,
		TuskVersion:   "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Tasks: []PortableTask{{
			ID:        uuid.New(),
			ShortID:   "ab123456",
			ProjectID: uuid.Nil,
			Title:     "no order",
			Status:    "pending",
			Order:     nil,
			Tags:      []string{},
			UDA:       nil,
		}},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, ws); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, buf.Bytes()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !strings.Contains(compact.String(), `"order":null`) {
		t.Errorf("expected `\"order\":null` in compacted output, got:\n%s", compact.String())
	}
}

func TestEncodeDecode_UrgencyOverridesNilOmitted(t *testing.T) {
	ws := &PortableWorkspace{
		SchemaVersion: SchemaVersion,
		TuskVersion:   "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Tasks: []PortableTask{{
			ID:               uuid.New(),
			ShortID:          "noover00",
			ProjectID:        uuid.Nil,
			Title:            "no overrides",
			Status:           "pending",
			Tags:             []string{},
			UDA:              map[string]string{},
			UrgencyOverrides: nil,
		}},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, ws); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(buf.String(), "urgency_overrides") {
		t.Errorf("expected urgency_overrides to be omitted, got:\n%s", buf.String())
	}

	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Tasks[0].UrgencyOverrides != nil {
		t.Errorf("expected UrgencyOverrides == nil after round-trip, got %+v", got.Tasks[0].UrgencyOverrides)
	}
}

func TestEncodeDecode_MultilineDescription(t *testing.T) {
	desc := "first\nsecond\nthird"
	ws := &PortableWorkspace{
		SchemaVersion: SchemaVersion,
		TuskVersion:   "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Tasks: []PortableTask{{
			ID:          uuid.New(),
			ShortID:     "multi001",
			ProjectID:   uuid.Nil,
			Title:       "multiline",
			Description: desc,
			Status:      "pending",
			Tags:        []string{},
			UDA:         map[string]string{},
		}},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, ws); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Tasks[0].Description != desc {
		t.Errorf("description mismatch\n want=%q\n got=%q", desc, got.Tasks[0].Description)
	}
}

func TestEncodeDecode_ClaimUnsetOmitted(t *testing.T) {
	ws := &PortableWorkspace{
		SchemaVersion: SchemaVersion,
		TuskVersion:   "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Tasks: []PortableTask{{
			ID:        uuid.New(),
			ShortID:   "noclaim0",
			ProjectID: uuid.Nil,
			Title:     "no claim",
			Status:    "pending",
			Tags:      []string{},
			UDA:       map[string]string{},
			ClaimedBy: nil,
			ClaimedAt: nil,
		}},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, ws); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "claimed_by") || strings.Contains(out, "claimed_at") {
		t.Errorf("expected claimed_by/claimed_at omitted, got:\n%s", out)
	}

	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Tasks[0].ClaimedBy != nil || got.Tasks[0].ClaimedAt != nil {
		t.Errorf("expected unset claim, got by=%v at=%v", got.Tasks[0].ClaimedBy, got.Tasks[0].ClaimedAt)
	}
}

func TestEncodeDecode_ClaimSetRoundTrip(t *testing.T) {
	claimedAt := time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
	ws := &PortableWorkspace{
		SchemaVersion: SchemaVersion,
		TuskVersion:   "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Tasks: []PortableTask{{
			ID:        uuid.New(),
			ShortID:   "claim001",
			ProjectID: uuid.Nil,
			Title:     "claimed",
			Status:    "active",
			Tags:      []string{},
			UDA:       map[string]string{},
			ClaimedBy: new("alice"),
			ClaimedAt: new(claimedAt),
		}},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, ws); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	gotTask := got.Tasks[0]
	if gotTask.ClaimedBy == nil || *gotTask.ClaimedBy != "alice" {
		t.Errorf("ClaimedBy mismatch: got %v", gotTask.ClaimedBy)
	}
	if gotTask.ClaimedAt == nil || !gotTask.ClaimedAt.Equal(claimedAt) {
		t.Errorf("ClaimedAt mismatch: got %v want %v", gotTask.ClaimedAt, claimedAt)
	}
}

// TestEncodeDecode_UDAEmptyAndNil documents the codec's behavior for the
// two "empty" forms of the UDA map. The round-trip preserves the
// distinction: a nil map encodes as JSON `null` and decodes back to
// nil; an empty map encodes as `{}` and decodes back to a non-nil but
// empty map. Consumers of the codec who treat these as equivalent
// should normalize after Decode.
func TestEncodeDecode_UDAEmptyAndNil(t *testing.T) {
	t.Run("nil_stays_nil", func(t *testing.T) {
		ws := &PortableWorkspace{
			SchemaVersion: SchemaVersion,
			TuskVersion:   "v0.13.0",
			ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
			Tasks: []PortableTask{{
				ID:        uuid.New(),
				ShortID:   "nilud001",
				ProjectID: uuid.Nil,
				Title:     "nil uda",
				Status:    "pending",
				Tags:      []string{},
				UDA:       nil,
			}},
		}
		var buf bytes.Buffer
		if err := Encode(&buf, ws); err != nil {
			t.Fatalf("Encode: %v", err)
		}
		got, err := Decode(&buf)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got.Tasks[0].UDA != nil {
			t.Errorf("expected UDA == nil, got %#v", got.Tasks[0].UDA)
		}
	})

	t.Run("empty_stays_empty", func(t *testing.T) {
		ws := &PortableWorkspace{
			SchemaVersion: SchemaVersion,
			TuskVersion:   "v0.13.0",
			ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
			Tasks: []PortableTask{{
				ID:        uuid.New(),
				ShortID:   "emptud01",
				ProjectID: uuid.Nil,
				Title:     "empty uda",
				Status:    "pending",
				Tags:      []string{},
				UDA:       map[string]string{},
			}},
		}
		var buf bytes.Buffer
		if err := Encode(&buf, ws); err != nil {
			t.Fatalf("Encode: %v", err)
		}
		got, err := Decode(&buf)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got.Tasks[0].UDA == nil {
			t.Errorf("expected UDA == empty map (non-nil), got nil")
		}
		if len(got.Tasks[0].UDA) != 0 {
			t.Errorf("expected len(UDA) == 0, got %d", len(got.Tasks[0].UDA))
		}
	})
}

func TestEncodeDecode_UnknownEventTypeRoundTrip(t *testing.T) {
	payload := json.RawMessage(`{"future_field":42,"nested":{"a":"b"}}`)
	ws := &PortableWorkspace{
		SchemaVersion: SchemaVersion,
		TuskVersion:   "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Events: []PortableEvent{{
			ID:         uuid.New(),
			Type:       "future_event_kind",
			EntityID:   "abc",
			EntityKind: "future_kind",
			PlayerID:   nil,
			Payload:    payload,
			CreatedAt:  time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		}},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, ws); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Events[0].Type != "future_event_kind" {
		t.Errorf("Type mismatch: got %q", got.Events[0].Type)
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, got.Events[0].Payload); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if compact.String() != string(payload) {
		t.Errorf("payload round-trip mismatch\n want=%s\n got=%s", payload, compact.String())
	}
}

func TestEncode_HTMLSpecialCharsLiteral(t *testing.T) {
	ws := &PortableWorkspace{
		SchemaVersion: SchemaVersion,
		TuskVersion:   "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Tasks: []PortableTask{{
			ID:          uuid.New(),
			ShortID:     "html0001",
			ProjectID:   uuid.Nil,
			Title:       "html",
			Description: "<a href=\"x\">click</a> & more",
			Status:      "pending",
			Tags:        []string{},
			UDA:         map[string]string{},
		}},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, ws); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out := buf.String()
	for _, ch := range []string{"<a href=", "click</a>", "& more"} {
		if !strings.Contains(out, ch) {
			t.Errorf("expected literal %q in output, got:\n%s", ch, out)
		}
	}
	for _, esc := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(out, esc) {
			t.Errorf("expected no JSON unicode escape %q in output, got:\n%s", esc, out)
		}
	}

	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Tasks[0].Description != ws.Tasks[0].Description {
		t.Errorf("description mismatch: got %q want %q",
			got.Tasks[0].Description, ws.Tasks[0].Description)
	}
}
