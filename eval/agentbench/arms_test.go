package main

import (
	"slices"
	"strings"
	"testing"
)

func TestToolArgs(test *testing.T) {
	arms, lookupErr := LookupArms("grep,tusk")

	if lookupErr != nil {
		test.Fatal(lookupErr)
	}

	serverTools := []string{"tusk_query", "tusk_node_get", "tusk_reset"}

	grepArgs := strings.Join(arms[0].ToolArgs(serverTools, "/stage/mcp.json"), " ")

	if grepArgs != "--tools Grep,Glob,Read --allowedTools Grep,Glob,Read" {
		test.Errorf("grep args = %q", grepArgs)
	}

	tuskArgs := strings.Join(arms[1].ToolArgs(serverTools, "/stage/mcp.json"), " ")
	want := "--tools Read --mcp-config /stage/mcp.json --allowedTools Read,mcp__tusk__tusk_query " +
		"--disallowedTools mcp__tusk__tusk_node_get,mcp__tusk__tusk_reset"

	if tuskArgs != want {
		test.Errorf("tusk args = %q\nwant        %q", tuskArgs, want)
	}
}

func TestLookupArmsRejectsUnknown(test *testing.T) {
	if _, lookupErr := LookupArms("grep,vector"); lookupErr == nil {
		test.Error("expected an error for an unknown arm")
	}
}

func TestCheckTools(test *testing.T) {
	arms, _ := LookupArms("tusk")
	arm := arms[0]

	if !slices.Equal(arm.ExpectedTools(), []string{"Read", "StructuredOutput", "mcp__tusk__tusk_query"}) {
		test.Errorf("expected tools = %v", arm.ExpectedTools())
	}

	if checkErr := arm.CheckTools([]string{"mcp__tusk__tusk_query", "StructuredOutput", "Read"}); checkErr != nil {
		test.Errorf("matching list rejected: %v", checkErr)
	}

	if checkErr := arm.CheckTools([]string{"Read", "StructuredOutput", "mcp__tusk__tusk_query", "mcp__tusk__tusk_node_get"}); checkErr == nil {
		test.Error("an extra tusk tool slipped through")
	}

	if checkErr := arm.CheckTools([]string{"Read", "StructuredOutput"}); checkErr == nil {
		test.Error("a missing tusk_query slipped through")
	}
}
