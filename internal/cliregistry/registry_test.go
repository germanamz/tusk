package cliregistry_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/cliregistry"
)

// TestReadOnlyTools asserts the MCP tool names match the expected wiring. The
// registry is the single source of truth that ties CLI verbs to MCP tools;
// changing a tool name without updating the registry breaks the alias
// dispatcher (Phase 1, Task 3).
func TestReadOnlyTools(test *testing.T) {
	expected := map[string]string{
		"node list":   "tusk_node_list",
		"node get":    "tusk_node_get",
		"node render": "tusk_node_render",
		"query":       "tusk_query",
		"edge list":   "tusk_edge_list",
		"doctor":      "tusk_doctor",
		"status":      "tusk_status",
	}

	for verb, wantTool := range expected {
		spec, present := cliregistry.ReadOnly[verb]

		if !present {
			test.Errorf("ReadOnly[%q] missing", verb)

			continue
		}

		if spec.Tool != wantTool {
			test.Errorf("ReadOnly[%q].Tool = %q, want %q", verb, spec.Tool, wantTool)
		}

		if !spec.ReadOnly {
			test.Errorf("ReadOnly[%q].ReadOnly = false, want true", verb)
		}

		if spec.Verb != verb {
			test.Errorf("ReadOnly[%q].Verb = %q, want %q", verb, spec.Verb, verb)
		}
	}
}

// TestReadOnlyPositionals asserts each entry's Positionals slice matches the
// Cobra signature for that verb. Updating one without the other will desync
// the alias dispatcher's argument decoder.
//
// The loop iterates cliregistry.ReadOnly (not cases) so that adding a new
// entry without a matching test case fails loudly rather than passing
// silently.
func TestReadOnlyPositionals(test *testing.T) {
	cases := map[string][]string{
		"node list":   {"filter"},
		"node get":    {"id"},
		"node render": {"id"},
		"query":       {"filter"},
		"edge list":   nil,
		"doctor":      nil,
		"status":      nil,
	}

	for verb, spec := range cliregistry.ReadOnly {
		wantPositionals, hasCase := cases[verb]

		if !hasCase {
			test.Errorf("no positionals test case for ReadOnly[%q] — add one to TestReadOnlyPositionals.cases", verb)

			continue
		}

		if len(spec.Positionals) != len(wantPositionals) {
			test.Errorf("ReadOnly[%q].Positionals len = %d, want %d", verb, len(spec.Positionals), len(wantPositionals))

			continue
		}

		for idx, name := range wantPositionals {
			if spec.Positionals[idx] != name {
				test.Errorf("ReadOnly[%q].Positionals[%d] = %q, want %q", verb, idx, spec.Positionals[idx], name)
			}
		}
	}
}

// TestWriteVerbsMarkedNotReadOnly asserts no Write entry has ReadOnly=true.
// The alias dispatcher's rejection check assumes this invariant.
func TestWriteVerbsMarkedNotReadOnly(test *testing.T) {
	wantVerbs := []string{
		"node create",
		"node modify",
		"node move",
		"node delete",
		"edge add",
		"edge remove",
	}

	for _, verb := range wantVerbs {
		spec, present := cliregistry.Write[verb]

		if !present {
			test.Errorf("Write[%q] missing", verb)

			continue
		}

		if spec.ReadOnly {
			test.Errorf("Write[%q].ReadOnly = true, want false", verb)
		}

		if spec.Verb != verb {
			test.Errorf("Write[%q].Verb = %q, want %q", verb, spec.Verb, verb)
		}
	}
}

// TestNoOverlap asserts no verb appears in both ReadOnly and Write maps. The
// dispatcher uses presence in ReadOnly as the "allow alias" signal; an overlap
// would create ambiguity.
func TestNoOverlap(test *testing.T) {
	for verb := range cliregistry.ReadOnly {
		if _, dup := cliregistry.Write[verb]; dup {
			test.Errorf("verb %q appears in both ReadOnly and Write", verb)
		}
	}
}
