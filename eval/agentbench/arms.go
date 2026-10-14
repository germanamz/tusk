package main

import (
	"fmt"
	"slices"
	"strings"
)

// tuskServer is the name the tusk MCP server is registered under, which
// prefixes its tool names (mcp__tusk__tusk_query).
const tuskServer = "tusk"

// Arm is one way of equipping the agent: the built-in Claude Code tools it
// gets and the tusk MCP tools it gets. Everything else is withheld.
type Arm struct {
	Name    string
	Builtin []string
	Tusk    []string
}

// knownArms are the arms a run can select. grep and tusk are the headline
// comparison; both is how tusk is usually deployed, next to grep rather
// than instead of it.
var knownArms = []Arm{
	{Name: "grep", Builtin: []string{"Grep", "Glob", "Read"}},
	{Name: "tusk", Builtin: []string{"Read"}, Tusk: []string{"tusk_query"}},
	{Name: "both", Builtin: []string{"Grep", "Glob", "Read"}, Tusk: []string{"tusk_query"}},
}

// baselineArm is the arm every other arm is compared against.
const baselineArm = "grep"

// LookupArms resolves a comma-separated list of arm names.
func LookupArms(names string) ([]Arm, error) {
	var arms []Arm

	for name := range strings.SplitSeq(names, ",") {
		name = strings.TrimSpace(name)

		index := slices.IndexFunc(knownArms, func(arm Arm) bool { return arm.Name == name })

		if index < 0 {
			return nil, fmt.Errorf("unknown arm %q (known: grep, tusk, both)", name)
		}

		arms = append(arms, knownArms[index])
	}

	return arms, nil
}

// UsesTusk reports whether the arm needs the tusk MCP server.
func (arm Arm) UsesTusk() bool {
	return len(arm.Tusk) > 0
}

func mcpToolName(tool string) string {
	return "mcp__" + tuskServer + "__" + tool
}

// ToolArgs returns the claude flags that give the agent this arm's tools.
// serverTools is every tool the tusk server offers; each one the arm doesn't
// use is disallowed, which removes it from the agent's tool list rather
// than just denying the call.
func (arm Arm) ToolArgs(serverTools []string, mcpConfig string) []string {
	allowed := slices.Clone(arm.Builtin)

	args := []string{"--tools", strings.Join(arm.Builtin, ",")}

	if !arm.UsesTusk() {
		return append(args, "--allowedTools", strings.Join(allowed, ","))
	}

	var disallowed []string

	for _, tool := range serverTools {
		if slices.Contains(arm.Tusk, tool) {
			allowed = append(allowed, mcpToolName(tool))
		} else {
			disallowed = append(disallowed, mcpToolName(tool))
		}
	}

	args = append(args, "--mcp-config", mcpConfig, "--allowedTools", strings.Join(allowed, ","))

	if len(disallowed) > 0 {
		args = append(args, "--disallowedTools", strings.Join(disallowed, ","))
	}

	return args
}

// ExpectedTools is the exact tool list the run's init event must report.
func (arm Arm) ExpectedTools() []string {
	expected := slices.Clone(arm.Builtin)
	expected = append(expected, structuredOutputTool)

	for _, tool := range arm.Tusk {
		expected = append(expected, mcpToolName(tool))
	}

	slices.Sort(expected)

	return expected
}

// CheckTools compares the tools a run reported against the arm. A mismatch
// means the arms aren't measuring what they claim (a new built-in tool, a
// tusk tool that slipped through), so it fails the whole benchmark.
func (arm Arm) CheckTools(reported []string) error {
	expected := arm.ExpectedTools()

	var missing, unexpected []string

	for _, tool := range expected {
		if !slices.Contains(reported, tool) {
			missing = append(missing, tool)
		}
	}

	for _, tool := range reported {
		if !slices.Contains(expected, tool) {
			unexpected = append(unexpected, tool)
		}
	}

	if len(missing) == 0 && len(unexpected) == 0 {
		return nil
	}

	return fmt.Errorf("arm %s: tool list mismatch (missing %v, unexpected %v)", arm.Name, missing, unexpected)
}
