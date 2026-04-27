// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"strings"
	"testing"
)

// TestTreeMarkdown_StubOutput exercises the Phase 3 markdown stub end-to-end.
// It uses runTusk (single-shot, not the dbMode×format harness) because
// --format markdown overrides anything the harness would otherwise set, and
// the four-way matrix would just run the same scenario four times.
func TestTreeMarkdown_StubOutput(t *testing.T) {
	dbPath := freshDBPath(t)

	mustRunTusk(t, dbPath,
		"project", "create", "roadmap",
		"workflow=kanban",
		`description="Roadmap-driven product backlog"`,
	)
	mustRunTusk(t, dbPath,
		"task", "create", "Ship phase 3",
		"project=roadmap",
	)

	// `task tree` does not accept a filter as a positional arg; the first
	// positional is interpreted as a short_id. Rely on the workspace having
	// tasks in only the `roadmap` project (the seeded `default` project has
	// none) so the single-project guard targets `roadmap`.
	out := mustRunTusk(t, dbPath,
		"task", "tree",
		"--format", "markdown",
	)

	if !strings.HasPrefix(out, "# Roadmap\n") {
		t.Fatalf("expected output to start with `# Roadmap`, got:\n%s", out)
	}
	if !strings.Contains(out, "Roadmap-driven product backlog") {
		t.Fatalf("expected description text in blockquote, got:\n%s", out)
	}
	if !strings.Contains(out, "<!-- tusk: markdown body lands in phase 4 -->") {
		t.Fatalf("expected phase-4 placeholder comment, got:\n%s", out)
	}
	// Phase 3 stub must not render task content yet.
	if strings.Contains(out, "Ship phase 3") {
		t.Fatalf("phase-3 stub should not render task content, got:\n%s", out)
	}
}
