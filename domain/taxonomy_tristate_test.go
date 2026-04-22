package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectSettings_Taxonomy_TristateNil(t *testing.T) {
	s := ProjectSettings{}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "taxonomy") {
		t.Fatalf("nil taxonomy should omit the key entirely, got %s", data)
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Taxonomy != nil {
		t.Fatalf("expected Taxonomy to be nil after round-trip, got %#v", decoded.Taxonomy)
	}
}

func TestProjectSettings_Taxonomy_TristateEmpty(t *testing.T) {
	empty := Taxonomy{}
	s := ProjectSettings{Taxonomy: &empty}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"taxonomy":[]`) {
		t.Fatalf("expected empty array encoding, got %s", data)
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Taxonomy == nil {
		t.Fatal("expected non-nil Taxonomy after round-trip")
	}
	if len(*decoded.Taxonomy) != 0 {
		t.Fatalf("expected empty taxonomy, got %#v", *decoded.Taxonomy)
	}
}

func TestProjectSettings_Taxonomy_TristatePopulated(t *testing.T) {
	populated := Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	s := ProjectSettings{Taxonomy: &populated}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"taxonomy":[["milestone"],["story"],["task","spike"]]`) {
		t.Fatalf("unexpected encoding: %s", data)
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Taxonomy == nil {
		t.Fatal("expected non-nil Taxonomy after round-trip")
	}
	got := *decoded.Taxonomy
	if len(got) != 3 || got[0][0] != "milestone" || got[2][1] != "spike" {
		t.Fatalf("unexpected round-trip result: %#v", got)
	}
}
