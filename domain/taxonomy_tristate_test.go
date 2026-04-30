package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectSettings_Taxonomy_TristateNil(test *testing.T) {
	settings := ProjectSettings{}

	data, err := json.Marshal(settings)

	if err != nil {
		test.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), "taxonomy") {
		test.Fatalf("nil taxonomy should omit the key entirely, got %s", data)
	}

	var decoded ProjectSettings

	if err := json.Unmarshal(data, &decoded); err != nil {
		test.Fatalf("unmarshal: %v", err)
	}
	if decoded.Taxonomy != nil {
		test.Fatalf("expected Taxonomy to be nil after round-trip, got %#v", decoded.Taxonomy)
	}
}

func TestProjectSettings_Taxonomy_TristateEmpty(test *testing.T) {
	empty := Taxonomy{}
	settings := ProjectSettings{Taxonomy: &empty}

	data, err := json.Marshal(settings)

	if err != nil {
		test.Fatalf("marshal: %v", err)
	}

	if !strings.Contains(string(data), `"taxonomy":[]`) {
		test.Fatalf("expected empty array encoding, got %s", data)
	}

	var decoded ProjectSettings

	if err := json.Unmarshal(data, &decoded); err != nil {
		test.Fatalf("unmarshal: %v", err)
	}
	if decoded.Taxonomy == nil {
		test.Fatal("expected non-nil Taxonomy after round-trip")
	}
	if len(*decoded.Taxonomy) != 0 {
		test.Fatalf("expected empty taxonomy, got %#v", *decoded.Taxonomy)
	}
}

func TestProjectSettings_Taxonomy_TristatePopulated(test *testing.T) {
	populated := Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	settings := ProjectSettings{Taxonomy: &populated}

	data, err := json.Marshal(settings)

	if err != nil {
		test.Fatalf("marshal: %v", err)
	}

	if !strings.Contains(string(data), `"taxonomy":[["milestone"],["story"],["task","spike"]]`) {
		test.Fatalf("unexpected encoding: %s", data)
	}

	var decoded ProjectSettings

	if err := json.Unmarshal(data, &decoded); err != nil {
		test.Fatalf("unmarshal: %v", err)
	}
	if decoded.Taxonomy == nil {
		test.Fatal("expected non-nil Taxonomy after round-trip")
	}
	got := *decoded.Taxonomy
	if len(got) != 3 || got[0][0] != "milestone" || got[2][1] != "spike" {
		test.Fatalf("unexpected round-trip result: %#v", got)
	}
}
