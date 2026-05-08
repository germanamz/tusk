package node_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

func TestParseFile_ExtractsFrontmatterAndBody(test *testing.T) {
	content := []byte(`---
type: ticket
title: Fix login bug
priority: 3
---

# Fix login bug

The body.
`)

	parsed, parseErr := node.ParseFile("tickets/fix-login-bug.md", content)

	if parseErr != nil {
		test.Fatalf("ParseFile: %v", parseErr)
	}

	if parsed.ID != "tickets/fix-login-bug" {
		test.Errorf("ID = %q", parsed.ID)
	}

	if parsed.Type != "ticket" {
		test.Errorf("Type = %q", parsed.Type)
	}

	if parsed.Title != "Fix login bug" {
		test.Errorf("Title = %q", parsed.Title)
	}

	priority, ok := parsed.Properties["priority"]

	if !ok {
		test.Fatalf("priority not in Properties")
	}

	if priorityInt, isInt := priority.(int); !isInt || priorityInt != 3 {
		test.Errorf("priority = %v (%T), want 3 (int)", priority, priority)
	}

	if string(parsed.Body) != "# Fix login bug\n\nThe body.\n" {
		test.Errorf("Body = %q", string(parsed.Body))
	}
}

func TestParseFile_HandlesNoFrontmatter(test *testing.T) {
	content := []byte("# Just a body\n\nNo frontmatter.\n")

	_, parseErr := node.ParseFile("notes/plain.md", content)

	if parseErr != node.ErrMissingFrontmatter {
		test.Errorf("err = %v, want ErrMissingFrontmatter", parseErr)
	}
}

func TestParseFile_RequiresTypeField(test *testing.T) {
	content := []byte(`---
title: missing type
---

body
`)

	_, parseErr := node.ParseFile("x.md", content)

	if parseErr != node.ErrMissingType {
		test.Errorf("err = %v, want ErrMissingType", parseErr)
	}
}

func TestParseFile_StripsExtensionFromID(test *testing.T) {
	content := []byte("---\ntype: note\n---\n\nbody\n")

	parsed, parseErr := node.ParseFile("a/b/c.md", content)

	if parseErr != nil {
		test.Fatalf("ParseFile: %v", parseErr)
	}

	if parsed.ID != "a/b/c" {
		test.Errorf("ID = %q, want a/b/c", parsed.ID)
	}
}
