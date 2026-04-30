package domain

import (
	"encoding/json"
	"testing"
)

func TestProjectSettings_JSONRoundTrip_Empty(test *testing.T) {
	// Default settings should serialize to `{}` and back
	var settings ProjectSettings

	data, err := json.Marshal(settings)

	if err != nil {
		test.Fatalf("marshal: %v", err)
	}

	if string(data) != "{}" {
		test.Fatalf("expected '{}', got %q", string(data))
	}

	var decoded ProjectSettings

	if err := json.Unmarshal(data, &decoded); err != nil {
		test.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent != nil {
		test.Fatal("expected AutoCompleteParent to be nil")
	}
	if decoded.AutoRevertParent != nil {
		test.Fatal("expected AutoRevertParent to be nil")
	}
}

func TestProjectSettings_JSONRoundTrip_WithConfig(test *testing.T) {
	settings := ProjectSettings{
		AutoCompleteParent: &AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "active",
		},
	}

	data, err := json.Marshal(settings)

	if err != nil {
		test.Fatalf("marshal: %v", err)
	}

	var decoded ProjectSettings

	if err := json.Unmarshal(data, &decoded); err != nil {
		test.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent == nil {
		test.Fatal("expected AutoCompleteParent to be non-nil")
	}
	if decoded.AutoCompleteParent.TriggerStatus != "completed" {
		test.Fatalf("expected trigger_status 'completed', got %q", decoded.AutoCompleteParent.TriggerStatus)
	}
	if decoded.AutoCompleteParent.TargetStatus != "completed" {
		test.Fatalf("expected target_status 'completed', got %q", decoded.AutoCompleteParent.TargetStatus)
	}
	if decoded.AutoRevertParent == nil {
		test.Fatal("expected AutoRevertParent to be non-nil")
	}
	if decoded.AutoRevertParent.TriggerStatus != "completed" {
		test.Fatalf("expected trigger_status 'completed', got %q", decoded.AutoRevertParent.TriggerStatus)
	}
	if decoded.AutoRevertParent.TargetStatus != "active" {
		test.Fatalf("expected target_status 'active', got %q", decoded.AutoRevertParent.TargetStatus)
	}
}

func TestProjectSettings_JSONRoundTrip_PartialConfig(test *testing.T) {
	// Only auto-complete set, auto-revert nil
	settings := ProjectSettings{
		AutoCompleteParent: &AutoCompleteConfig{
			TriggerStatus: "done",
			TargetStatus:  "done",
		},
	}

	data, err := json.Marshal(settings)

	if err != nil {
		test.Fatalf("marshal: %v", err)
	}

	var decoded ProjectSettings

	if err := json.Unmarshal(data, &decoded); err != nil {
		test.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent == nil {
		test.Fatal("expected AutoCompleteParent to be non-nil")
	}
	if decoded.AutoRevertParent != nil {
		test.Fatal("expected AutoRevertParent to be nil")
	}
}
