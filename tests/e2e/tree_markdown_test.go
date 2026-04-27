// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"strings"
	"testing"
)

// shortIDFromCreate parses the short ID out of a `tusk task create` output
// line of the form "Created task <short_id>\n".
func shortIDFromCreate(t *testing.T, out string) string {
	t.Helper()
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		t.Fatalf("unexpected create output: %q", out)
	}
	return fields[len(fields)-1]
}

// TestTreeMarkdown_FullDialect exercises the Phase 4 markdown body renderer
// end-to-end. The fixture seeds a four-level tree (milestone → initiative →
// stories → leaf tasks) with descriptions and varied statuses so the output
// stresses every rendering rule introduced in Phase 4: H1/H2/H3/bullet shape,
// status checkboxes, the `status=` token for non-binary statuses, and the
// blockquote indentation under bullets.
func TestTreeMarkdown_FullDialect(t *testing.T) {
	dbPath := freshDBPath(t)

	mustRunTusk(t, dbPath,
		"project", "create", "roadmap",
		"workflow=kanban",
		`description="Roadmap-driven product backlog"`,
	)

	milestoneID := shortIDFromCreate(t, mustRunTusk(t, dbPath,
		"task", "create", "v1 launch",
		"project=roadmap",
		`description="Ship the first public release."`,
	))

	initiativeID := shortIDFromCreate(t, mustRunTusk(t, dbPath,
		"task", "create", "Onboarding flow",
		"project=roadmap",
		`description="Make the first-run experience friendly."`,
		"parent="+milestoneID,
	))

	completedStoryID := shortIDFromCreate(t, mustRunTusk(t, dbPath,
		"task", "create", "Welcome screen",
		"project=roadmap",
		`description="Greet the user."`,
		"parent="+initiativeID,
	))

	activeStoryID := shortIDFromCreate(t, mustRunTusk(t, dbPath,
		"task", "create", "Sample data import",
		"project=roadmap",
		"parent="+initiativeID,
	))

	mustRunTusk(t, dbPath,
		"task", "create", "Wire CSV parser",
		"project=roadmap",
		"parent="+activeStoryID,
	)
	mustRunTusk(t, dbPath,
		"task", "create", "Add fixture pack",
		"project=roadmap",
		"parent="+activeStoryID,
	)
	mustRunTusk(t, dbPath,
		"task", "create", "Wire welcome copy",
		"project=roadmap",
		"parent="+completedStoryID,
	)
	mustRunTusk(t, dbPath,
		"task", "create", "Hook auth banner",
		"project=roadmap",
		"parent="+completedStoryID,
	)

	// Walk the kanban transitions: pending → active for the active story,
	// pending → active → completed for the completed story.
	mustRunTusk(t, dbPath, "task", "modify", activeStoryID, "status=active")
	mustRunTusk(t, dbPath, "task", "modify", completedStoryID, "status=active")
	mustRunTusk(t, dbPath, "task", "modify", completedStoryID, "status=completed")

	out := mustRunTusk(t, dbPath, "task", "tree", "--format", "markdown")

	if !strings.HasPrefix(out, "# Roadmap\n") {
		t.Fatalf("expected H1 for project, got:\n%s", out)
	}
	if !strings.Contains(out, "> Roadmap-driven product backlog") {
		t.Fatalf("expected description blockquote, got:\n%s", out)
	}
	if !strings.Contains(out, "## v1 launch") {
		t.Fatalf("expected ## milestone heading, got:\n%s", out)
	}
	if !strings.Contains(out, "### Onboarding flow") {
		t.Fatalf("expected ### initiative heading, got:\n%s", out)
	}
	if !strings.Contains(out, "- [x] Welcome screen") {
		t.Fatalf("expected completed-story checkbox `- [x] Welcome screen`, got:\n%s", out)
	}
	if !strings.Contains(out, "- [ ] Sample data import status=active") {
		t.Fatalf("expected active-story bullet with status=active, got:\n%s", out)
	}
	if !strings.Contains(out, "  - [ ] Wire CSV parser") {
		t.Fatalf("expected indented leaf bullet under active story, got:\n%s", out)
	}
	if strings.Contains(out, "<!-- tusk: markdown body lands in phase 4 -->") {
		t.Fatalf("phase-3 placeholder must be gone, got:\n%s", out)
	}
}
