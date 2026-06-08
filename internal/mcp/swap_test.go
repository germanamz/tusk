package mcp

import (
	"context"
	"testing"
)

func TestReopenInPlace_KeepsServing(test *testing.T) {
	srv := buildTestServer(test)

	before, beforeErr := srv.HandleToolCall(context.Background(), nodeListRequest())

	if beforeErr != nil {
		test.Fatalf("list before: %v", beforeErr)
	}

	if reopenErr := srv.reopenInPlace(); reopenErr != nil {
		test.Fatalf("reopenInPlace: %v", reopenErr)
	}

	after, afterErr := srv.HandleToolCall(context.Background(), nodeListRequest())

	if afterErr != nil {
		test.Fatalf("list after reopen: %v", afterErr)
	}

	if textOf(before) != textOf(after) {
		test.Fatalf("node list changed across a non-destructive reopen:\nbefore=%s\nafter=%s", textOf(before), textOf(after))
	}
}
