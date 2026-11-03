package manifest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// acyclic takes a severity as well as a bool (#852): true and "error" refuse
// a cycle on write and doctor reports it as an error; "warning" and "advice"
// only set doctor's level; false and "none" turn the check off.
func TestLoad_AcyclicTakesASeverity(test *testing.T) {
	loaded, loadErr := loadManifestBody(test, `[workspace]
name = "x"

[node-types.service]
properties = [
  { name = "replaces", type = "ref", to = "service", acyclic = "warning" },
]

[edge-types.bool-true]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
acyclic = true

[edge-types.named-error]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
acyclic = "error"

[edge-types.named-warning]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
acyclic = "warning"

[edge-types.named-advice]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
acyclic = "advice"

[edge-types.bool-false]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
acyclic = false

[edge-types.named-none]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
acyclic = "none"

[edge-types.unset]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
`)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	want := map[string]manifest.Acyclicity{
		"bool-true":     manifest.AcyclicError,
		"named-error":   manifest.AcyclicError,
		"named-warning": manifest.AcyclicWarning,
		"named-advice":  manifest.AcyclicAdvice,
		"bool-false":    manifest.AcyclicNone,
		"named-none":    manifest.AcyclicNone,
		"unset":         manifest.AcyclicNone,
		// A ref property's level carries over to its synthesized edge type.
		"replaces": manifest.AcyclicWarning,
	}

	for name, level := range want {
		if got := loaded.EdgeTypes[name].Acyclic; got != level {
			test.Errorf("%s: Acyclic = %q, want %q", name, got, level)
		}
	}

	for level, refuses := range map[manifest.Acyclicity]bool{
		manifest.AcyclicError:   true,
		manifest.AcyclicWarning: false,
		manifest.AcyclicAdvice:  false,
		manifest.AcyclicNone:    false,
	} {
		if level.RefusesCycles() != refuses {
			test.Errorf("%q.RefusesCycles() = %v, want %v", level, !refuses, refuses)
		}
	}
}

func TestLoad_RejectsAnUnknownAcyclicValue(test *testing.T) {
	for value, wantMessage := range map[string]string{
		`"warn"`: `acyclic "warn" is not one of true, false, "error", "warning", "advice", "none"`,
		`3`:      `acyclic takes true, false, "error", "warning", "advice" or "none", not 3`,
	} {
		_, loadErr := loadManifestBody(test, `[workspace]
name = "x"

[edge-types.depends-on]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
acyclic = `+value+`
`)

		if loadErr == nil || !strings.Contains(loadErr.Error(), wantMessage) {
			test.Errorf("acyclic = %s: Load error = %v, want it to contain %q", value, loadErr, wantMessage)
		}
	}
}

// The JSON a schema fingerprint hashes keeps acyclic a bool for the two
// levels a bool can spell, so a manifest written before #852 keeps its
// fingerprint and an upgrade does not force a full reindex.
func TestAcyclicity_JSONKeepsTheBoolShape(test *testing.T) {
	for level, want := range map[manifest.Acyclicity]string{
		manifest.AcyclicError:   `true`,
		manifest.AcyclicNone:    `false`,
		manifest.AcyclicWarning: `"warning"`,
		manifest.AcyclicAdvice:  `"advice"`,
	} {
		encoded, marshalErr := json.Marshal(level)

		if marshalErr != nil || string(encoded) != want {
			test.Errorf("json.Marshal(%q) = %s, %v, want %s", level, encoded, marshalErr, want)
		}
	}
}
