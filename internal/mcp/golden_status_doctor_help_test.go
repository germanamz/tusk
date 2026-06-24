package mcp_test

import (
	"os"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
)

// TestGoldenMCP_Status pins tusk_status: the {nodes_by_type, edge_count,
// embed/reindex queue depths, last_reindex_at} envelope (timestamp scrubbed).
func TestGoldenMCP_Status(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name:        "status on an empty workspace",
			tool:        "tusk_status",
			args:        map[string]any{},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"edge_count":0,"embed_queue_depth":0,"last_reindex_at":"<TS>","nodes_by_type":{},"reindex_queue_depth":0}`,
		},
		{
			name: "status counts nodes by type",
			setup: func(test *testing.T, rt *mcp.Runtime) {
				seedNode(test, rt, "notes/a", "note")
				seedNode(test, rt, "notes/b", "note")
				seedNode(test, rt, "tasks/t", "task")
			},
			tool:        "tusk_status",
			args:        map[string]any{},
			wantIsError: false,
			wantJSONObj: true,
			// Seeded rows only — no reindex, so no sub-units/edges/embed jobs.
			wantText: `{"edge_count":0,"embed_queue_depth":0,"last_reindex_at":"<TS>","nodes_by_type":{"note":2,"task":1},"reindex_queue_depth":0}`,
		},
	})
}

// TestGoldenMCP_Doctor pins tusk_doctor's clean-workspace envelope.
func TestGoldenMCP_Doctor(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name:        "doctor on a clean workspace",
			tool:        "tusk_doctor",
			args:        map[string]any{},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    goldenMCPDoctorClean,
		},
	})
}

// TestGoldenMCP_DoctorWorkflowViolation pins the structured issues array for a
// workflow-violation drift row — the MCP-side contract for #497. The row is
// seeded directly (so the envelope stays deterministic: no reindex, no embed
// jobs); the message is the validator's full rendered detail, which the issue's
// hardcoded doctor string could not produce.
func TestGoldenMCP_DoctorWorkflowViolation(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name: "doctor renders a workflow drift row's persisted detail",
			setup: func(test *testing.T, rt *mcp.Runtime) {
				if appendErr := rt.WorkflowDrift.Append(index.WorkflowDriftRow{
					NodeID:         "tickets/demo",
					PackInstance:   "kanban",
					PackKind:       "workflow",
					ObservedStatus: "bogus",
					Property:       "status",
					ErrorCode:      "unknown-target-state",
					Detail:         "workflow \"kanban\": \"bogus\" is not a declared state for property \"status\"\n  declared states: active, completed, pending",
					ObservedAt:     1,
				}); appendErr != nil {
					test.Fatalf("seed workflow drift: %v", appendErr)
				}
			},
			tool:        "tusk_doctor",
			args:        map[string]any{},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    goldenMCPDoctorWorkflowViolation,
		},
	})
}

// goldenMCPDoctorWorkflowViolation is goldenMCPDoctorClean with the issues array
// carrying the seeded workflow-violation row. The message is the validator's
// fully-rendered detail (escaped \n + "declared states:" continuation) — the
// structured MCP form of the #497 fix. Captured from a real run.
const goldenMCPDoctorWorkflowViolation = `{"embed_queue_depth":0,"graph_expansion":{"candidate_multiplier":5,"edge_types":["references","parent","tagged","contains"],"enabled":false,"hops":1,"unknown_edge_types":["parent","references","tagged"],"weight":0.2,"weight_zero_no_op":false},"issues":[{"kind":"workflow-violation","message":"workflow \"kanban\": \"bogus\" is not a declared state for property \"status\"\n  declared states: active, completed, pending","node_id":"tickets/demo"}],"migrated":null,"migrated_count":0,"reindex_queue_depth":0,"skipped":null,"skipped_count":0,"sub_units":{"count_by_kind":{},"deduped_sub_units":0,"embed_queue_files":0,"embed_queue_sub_units":0,"orphaned_sub_units":0,"oversize_embed_payloads":0,"total":0}}`

// goldenMCPDoctorClean is tusk_doctor's clean-workspace envelope. Note mcp.Open
// merges the builtin pack, so graph_expansion carries the same default edge
// types as the CLI; nil slices marshal to null (migrated/skipped) and empty maps
// to {} (count_by_kind) — captured from a real run, not fabricated.
const goldenMCPDoctorClean = `{"embed_queue_depth":0,"graph_expansion":{"candidate_multiplier":5,"edge_types":["references","parent","tagged","contains"],"enabled":false,"hops":1,"unknown_edge_types":["parent","references","tagged"],"weight":0.2,"weight_zero_no_op":false},"issues":[],"migrated":null,"migrated_count":0,"reindex_queue_depth":0,"skipped":null,"skipped_count":0,"sub_units":{"count_by_kind":{},"deduped_sub_units":0,"embed_queue_files":0,"embed_queue_sub_units":0,"orphaned_sub_units":0,"oversize_embed_payloads":0,"total":0}}`

// TestGoldenMCP_Help pins the small, deterministic help surface: the unknown
// topic path, which always succeeds with plain text and the sorted index.
func TestGoldenMCP_Help(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name:        "help with an unknown topic returns the sorted index",
			tool:        "tusk_help",
			args:        map[string]any{"topic": "nope"},
			wantIsError: false,
			wantJSONObj: false,
			wantText:    "Unknown topic \"nope\".\n\n" + goldenHelpIndex,
		},
	})
}

// TestGoldenMCP_HelpContent pins tusk_help's content against the embedded
// help/*.md source files — the actual contract (verbatim doc passthrough)
// without multi-KB inline literals. A doc edit correctly breaks these.
func TestGoldenMCP_HelpContent(test *testing.T) {
	rt := goldenRuntime(test, "")
	srv := mcp.NewServer(rt)

	for _, topic := range []string{"overview", "workflow", "node-types", "edge-types", "manifest", "filter", "query", "packs"} {
		want, readErr := os.ReadFile("help/" + topic + ".md")

		if readErr != nil {
			test.Fatalf("read help/%s.md: %v", topic, readErr)
		}

		text, isError := rawToolResult(test, srv, "tusk_help", map[string]any{"topic": topic})

		if isError {
			test.Errorf("tusk_help(%s) IsError = true, want success", topic)
		}

		if text != string(want) {
			test.Errorf("tusk_help(%s) drifted from help/%s.md", topic, topic)
		}
	}

	overview, readErr := os.ReadFile("help/overview.md")

	if readErr != nil {
		test.Fatalf("read help/overview.md: %v", readErr)
	}

	text, _ := rawToolResult(test, srv, "tusk_help", map[string]any{})

	if want := string(overview) + "\n\n" + goldenHelpIndex; text != want {
		test.Errorf("tusk_help() (no topic) is not overview.md + sorted index")
	}
}

// goldenHelpIndex is the exact sorted topic index helpTopicIndex() renders.
const goldenHelpIndex = `Available tusk_help topics:

  - edge-types
  - filter
  - manifest
  - node-types
  - overview
  - packs
  - query
  - workflow

Call tusk_help(topic: "<name>") to read one.`
