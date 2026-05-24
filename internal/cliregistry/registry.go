// Package cliregistry declares the canonical mapping between CLI verbs and
// their MCP tool counterparts. Each entry names the positional arguments the
// CLI accepts (in order) and the MCP tool that exposes the same operation.
//
// The registry is consumed by the alias dispatcher (added in a later phase) and
// by tests that verify the CLI and MCP surfaces stay in sync.
package cliregistry

// VerbSpec describes a single CLI sub-command and its MCP counterpart.
type VerbSpec struct {
	// Positionals lists the names of positional CLI arguments in order.
	// Nil or empty when the verb takes no positionals.
	Positionals []string
	// Tool is the MCP tool name (e.g. "tusk_node_list"). Empty for write
	// verbs that are intentionally not aliased.
	Tool string
	// Verb is the canonical CLI sub-command path (e.g. "node list").
	Verb string
	// ReadOnly is true for verbs that never mutate disk or index state.
	ReadOnly bool
}

// ReadOnly enumerates every read-only CLI verb the workspace exposes. The
// alias dispatcher (Phase 1, Task 3) consults this map to decide whether a
// verb may be projected through the user's alias namespace.
var ReadOnly = map[string]VerbSpec{
	"node list": {Positionals: []string{"filter"}, Tool: "tusk_node_list", Verb: "node list", ReadOnly: true},
	"node get":  {Positionals: []string{"id"}, Tool: "tusk_node_get", Verb: "node get", ReadOnly: true},
	"query":     {Positionals: []string{"filter"}, Tool: "tusk_query", Verb: "query", ReadOnly: true},
	"edge list": {Positionals: nil, Tool: "tusk_edge_list", Verb: "edge list", ReadOnly: true},
	"doctor":    {Positionals: nil, Tool: "tusk_doctor", Verb: "doctor", ReadOnly: true},
	"status":    {Positionals: nil, Tool: "tusk_status", Verb: "status", ReadOnly: true},
}

// Write enumerates the CLI verbs that mutate disk or index state. Listed so
// the alias dispatcher can reject them by name with a clear "write verbs are
// not aliased" error.
var Write = map[string]VerbSpec{
	"node create": {Verb: "node create", ReadOnly: false},
	"node modify": {Verb: "node modify", ReadOnly: false},
	"node move":   {Verb: "node move", ReadOnly: false},
	"node delete": {Verb: "node delete", ReadOnly: false},
	"edge add":    {Verb: "edge add", ReadOnly: false},
	"edge remove": {Verb: "edge remove", ReadOnly: false},
}
