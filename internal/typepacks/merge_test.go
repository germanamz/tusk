package typepacks_test

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/typepacks"
)

func TestFindCollisions_NoOverlap(test *testing.T) {
	user := []byte(`
[node-types.note]
properties = [{ name = "summary", type = "string" }]
`)

	pack := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"task": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
		},
	}

	collisions, findErr := typepacks.FindCollisions(user, pack)

	if findErr != nil {
		test.Fatalf("FindCollisions: %v", findErr)
	}

	if len(collisions) != 0 {
		test.Errorf("collisions = %v, want none", collisions)
	}
}

func TestFindCollisions_NodeTypeOverlap(test *testing.T) {
	user := []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }]
`)

	pack := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"task": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
		},
	}

	collisions, _ := typepacks.FindCollisions(user, pack)

	if len(collisions) != 1 || collisions[0] != "node-types.task" {
		test.Errorf("collisions = %v, want [node-types.task]", collisions)
	}
}

func TestFindCollisions_EdgeAndBehaviorOverlap(test *testing.T) {
	user := []byte(`
[edge-types.parent]
from = ["task"]
to = ["task"]
cardinality = "many-to-one"

[behaviors.workflow.kanban]
applies-to = ["task"]
`)

	pack := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{
			"parent": {From: []string{"task"}, To: []string{"task"}, Cardinality: manifest.CardinalityManyToOne},
		},
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"kanban": {}},
		},
	}

	collisions, _ := typepacks.FindCollisions(user, pack)

	wantSet := map[string]bool{"edge-types.parent": false, "behaviors.workflow.kanban": false}

	for _, key := range collisions {
		wantSet[key] = true
	}

	for key, found := range wantSet {
		if !found {
			test.Errorf("missing expected collision %q (got %v)", key, collisions)
		}
	}
}

func TestStripSections_RemovesNamedHeaders(test *testing.T) {
	body := []byte(`[workspace]
name = "ws"

[node-types.task]
properties = [{ name = "summary", type = "string" }]

[edge-types.parent]
from = ["task"]
to = ["task"]
cardinality = "many-to-one"

[node-types.note]
properties = [{ name = "summary", type = "string" }]
`)

	stripped := typepacks.StripSections(body, []string{"node-types.task", "edge-types.parent"})

	if strings.Contains(string(stripped), "[node-types.task]") {
		test.Errorf("stripped output still contains [node-types.task]: %q", stripped)
	}

	if strings.Contains(string(stripped), "[edge-types.parent]") {
		test.Errorf("stripped output still contains [edge-types.parent]: %q", stripped)
	}

	if !strings.Contains(string(stripped), "[node-types.note]") {
		test.Errorf("stripped output unexpectedly removed [node-types.note]: %q", stripped)
	}

	if !strings.Contains(string(stripped), "[workspace]") {
		test.Errorf("stripped output unexpectedly removed [workspace]: %q", stripped)
	}
}
