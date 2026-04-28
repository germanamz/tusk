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
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newEnv(t, binPath, "flag", "text")
	env.WithoutFormat()

	mustRun(t, env,
		"project", "create", "roadmap",
		"workflow=kanban",
		`description="Roadmap-driven product backlog"`,
	)

	milestoneID := shortIDFromCreate(t, mustRun(t, env,
		"task", "create", "v1 launch",
		"project=roadmap",
		`description="Ship the first public release."`,
	).Stdout)

	initiativeID := shortIDFromCreate(t, mustRun(t, env,
		"task", "create", "Onboarding flow",
		"project=roadmap",
		`description="Make the first-run experience friendly."`,
		"parent="+milestoneID,
	).Stdout)

	completedStoryID := shortIDFromCreate(t, mustRun(t, env,
		"task", "create", "Welcome screen",
		"project=roadmap",
		`description="Greet the user."`,
		"parent="+initiativeID,
	).Stdout)

	activeStoryID := shortIDFromCreate(t, mustRun(t, env,
		"task", "create", "Sample data import",
		"project=roadmap",
		"parent="+initiativeID,
	).Stdout)

	csvLeafID := shortIDFromCreate(t, mustRun(t, env,
		"task", "create", "Wire CSV parser",
		"project=roadmap",
		"parent="+activeStoryID,
	).Stdout)
	mustRun(t, env,
		"task", "create", "Add fixture pack",
		"project=roadmap",
		"parent="+activeStoryID,
	)
	mustRun(t, env,
		"task", "create", "Wire welcome copy",
		"project=roadmap",
		"parent="+completedStoryID,
	)
	mustRun(t, env,
		"task", "create", "Hook auth banner",
		"project=roadmap",
		"parent="+completedStoryID,
	)

	// Walk the kanban transitions: pending → active for the active story,
	// pending → active → completed for the completed story.
	mustRun(t, env, "task", "modify", activeStoryID, "status=active")
	mustRun(t, env, "task", "modify", completedStoryID, "status=active")
	mustRun(t, env, "task", "modify", completedStoryID, "status=completed")

	// Phase 5: annotations and notes — milestone gets an annotation, the
	// project gets a project-level note, and the CSV-parser leaf gets a
	// per-task note.
	mustRun(t, env, "task", "annotate", milestoneID, "Initial scope ratified")
	mustRun(t, env, "note", "add", "caching strategy notes",
		"project=roadmap", "--player", "german",
	)
	mustRun(t, env, "note", "add", "retry needed",
		"project=roadmap", "--player", "german", "--task", csvLeafID,
	)

	out := mustRun(t, env, "task", "tree", "--format", "markdown").Stdout

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

	// Phase 5 additions: annotations and notes labels and bodies.
	if !strings.Contains(out, "**Annotations:**") {
		t.Fatalf("expected **Annotations:** label, got:\n%s", out)
	}
	if !strings.Contains(out, "**Notes:**") {
		t.Fatalf("expected **Notes:** label, got:\n%s", out)
	}
	if !strings.Contains(out, "Initial scope ratified") {
		t.Fatalf("expected milestone annotation body, got:\n%s", out)
	}
	if !strings.Contains(out, "caching strategy notes") {
		t.Fatalf("expected project-level note body, got:\n%s", out)
	}
	if !strings.Contains(out, "retry needed") {
		t.Fatalf("expected leaf-task note body, got:\n%s", out)
	}

	// `tusk task tree project=<name>` accepts the project filter as inline
	// syntax (per the v0.13 design spec). It must produce the same render as
	// the bare invocation when the workspace has tasks for one project only —
	// every fixture task above belongs to `roadmap`.
	filteredOut := mustRun(t, env,
		"task", "tree", "project=roadmap", "--format", "markdown",
	).Stdout
	if filteredOut != out {
		t.Fatalf("project=<name> filter should match bare invocation in single-project workspace.\nbare:\n%s\nfiltered:\n%s", out, filteredOut)
	}
}
