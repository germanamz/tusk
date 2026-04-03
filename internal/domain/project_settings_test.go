package domain

import (
	"encoding/json"
	"testing"
)

func TestProjectSettings_JSONRoundTrip_Empty(t *testing.T) {
	// Default settings should serialize to `{}` and back
	var s ProjectSettings
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("expected '{}', got %q", string(data))
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent != nil {
		t.Fatal("expected AutoCompleteParent to be nil")
	}
	if decoded.AutoRevertParent != nil {
		t.Fatal("expected AutoRevertParent to be nil")
	}
}

func TestProjectSettings_JSONRoundTrip_WithConfig(t *testing.T) {
	s := ProjectSettings{
		AutoCompleteParent: &AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "active",
		},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be non-nil")
	}
	if decoded.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", decoded.AutoCompleteParent.TriggerStatus)
	}
	if decoded.AutoCompleteParent.TargetStatus != "completed" {
		t.Fatalf("expected target_status 'completed', got %q", decoded.AutoCompleteParent.TargetStatus)
	}
	if decoded.AutoRevertParent == nil {
		t.Fatal("expected AutoRevertParent to be non-nil")
	}
	if decoded.AutoRevertParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", decoded.AutoRevertParent.TriggerStatus)
	}
	if decoded.AutoRevertParent.TargetStatus != "active" {
		t.Fatalf("expected target_status 'active', got %q", decoded.AutoRevertParent.TargetStatus)
	}
}

func TestProjectSettings_JSONRoundTrip_PartialConfig(t *testing.T) {
	// Only auto-complete set, auto-revert nil
	s := ProjectSettings{
		AutoCompleteParent: &AutoCompleteConfig{
			TriggerStatus: "done",
			TargetStatus:  "done",
		},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be non-nil")
	}
	if decoded.AutoRevertParent != nil {
		t.Fatal("expected AutoRevertParent to be nil")
	}
}
