package workflow_test

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/behavior/workflow"
)

const sampleManifest = `
[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`

func TestKind_NewInstance_HappyPath(test *testing.T) {
	var decoded struct {
		Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`
	}

	meta, decodeErr := toml.Decode(sampleManifest, &decoded)

	if decodeErr != nil {
		test.Fatalf("toml decode: %v", decodeErr)
	}

	primitive := decoded.Behaviors["workflow"]["tickets"]

	kind := workflow.Kind{}
	instance, newErr := kind.NewInstance("tickets", primitive, &meta)

	if newErr != nil {
		test.Fatalf("NewInstance: %v", newErr)
	}

	if instance.Name() != "tickets" || instance.Kind() != "workflow" {
		test.Errorf("instance qualifier = %s.%s, want workflow.tickets", instance.Kind(), instance.Name())
	}

	reserved := instance.ReservedKeys()

	if len(reserved) != 1 || reserved[0].NodeType != "ticket" || reserved[0].Property != "status" {
		test.Errorf("ReservedKeys = %v, want [{ticket status}]", reserved)
	}

	hooks := instance.Hooks()

	if hooks.OnNodeWriteValidate == nil {
		test.Errorf("Hooks.OnNodeWriteValidate is nil")
	}
}

func TestKind_NewInstance_DecodeError(test *testing.T) {
	const broken = `
[behaviors.workflow.bad]
applies-to = ["ticket"]
# states missing → schema rejection
`

	var decoded struct {
		Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`
	}

	meta, decodeErr := toml.Decode(broken, &decoded)

	if decodeErr != nil {
		test.Fatalf("toml decode: %v", decodeErr)
	}

	primitive := decoded.Behaviors["workflow"]["bad"]

	kind := workflow.Kind{}
	_, newErr := kind.NewInstance("bad", primitive, &meta)

	if newErr == nil || !strings.Contains(newErr.Error(), "states") {
		test.Errorf("NewInstance: expected states error, got %v", newErr)
	}
}
