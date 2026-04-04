package e2e

import (
	"strings"
	"testing"
)

func TestProjectCommands(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "project_list_default",
			Steps: []Step{
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "_default")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) < 1 {
							t.Fatal("expected at least 1 project (_default)")
						}
						found := false
						for _, item := range arr {
							m := item.(map[string]any)
							if m["name"] == "_default" {
								found = true
								break
							}
						}
						if !found {
							t.Fatal("expected _default project in list")
						}
					},
				},
			},
		},
		{
			Name: "project_create_and_list",
			Steps: []Step{
				// Step 0: Create a project
				{
					Args: []string{"project", "create", "myproject", "-d", "My test project"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created project myproject")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "myproject")
						assertEqual(t, m["description"], "My test project")
						assertEqual(t, m["default_workflow"], "default")
						assertEqual(t, m["version"], float64(1))
					},
				},
				// Step 1: List should include the new project
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "myproject")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						// _default + myproject = 2
						if len(arr) != 2 {
							t.Fatalf("expected 2 projects, got %d", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "project_modify_description",
			Steps: []Step{
				// Step 0: Create
				{Args: []string{"project", "create", "descproj", "-d", "Old desc"}},
				// Step 1: Modify description
				{
					Args: []string{"project", "modify", "descproj", "-d", "New desc"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified project descproj")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["description"], "New desc")
						assertEqual(t, m["version"], float64(2))
					},
				},
			},
		},
		{
			Name: "project_modify_settings_set",
			Steps: []Step{
				// Step 0: Create
				{Args: []string{"project", "create", "settingsproj"}},
				// Step 1: Set auto-complete settings
				{
					Args: []string{
						"project", "modify", "settingsproj",
						"--set", "auto_complete_parent.trigger_status=completed",
						"--set", "auto_complete_parent.target_status=completed",
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						settings := m["settings"].(map[string]any)
						ac := settings["auto_complete_parent"].(map[string]any)
						assertEqual(t, ac["trigger_status"], "completed")
						assertEqual(t, ac["target_status"], "completed")
					},
				},
			},
		},
		{
			Name: "project_modify_settings_unset",
			Steps: []Step{
				// Step 0: Create
				{Args: []string{"project", "create", "unsetproj"}},
				// Step 1: Set auto-complete
				{Args: []string{
					"project", "modify", "unsetproj",
					"--set", "auto_complete_parent.trigger_status=completed",
					"--set", "auto_complete_parent.target_status=completed",
				}},
				// Step 2: Unset auto-complete
				{
					Args: []string{
						"project", "modify", "unsetproj",
						"--unset", "auto_complete_parent",
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						settings := m["settings"].(map[string]any)
						if settings["auto_complete_parent"] != nil {
							t.Fatal("expected auto_complete_parent to be nil after unset")
						}
					},
				},
			},
		},
		{
			Name: "project_create_duplicate",
			Steps: []Step{
				{Args: []string{"project", "create", "dupproj"}},
				{
					Args:    []string{"project", "create", "dupproj"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_modify_not_found",
			Steps: []Step{
				{
					Args:    []string{"project", "modify", "nonexistent", "-d", "nope"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_modify_no_flags",
			Steps: []Step{
				{
					Args:    []string{"project", "modify", "_default"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_modify_invalid_set_format",
			Steps: []Step{
				{
					Args:    []string{"project", "modify", "_default", "--set", "noequals"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_modify_unknown_set_path",
			Steps: []Step{
				{
					Args:    []string{"project", "modify", "_default", "--set", "unknown.path=value"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_create_description_in_list",
			Steps: []Step{
				{Args: []string{"project", "create", "listedproj", "-d", "Visible description"}},
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "listedproj")
						assertContains(t, output, "Visible description")
					},
				},
			},
		},
		{
			Name: "project_settings_summary_in_list",
			Steps: []Step{
				{Args: []string{"project", "create", "summaryproj"}},
				{Args: []string{
					"project", "modify", "summaryproj",
					"--set", "auto_complete_parent.trigger_status=completed",
					"--set", "auto_complete_parent.target_status=completed",
				}},
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Find the line for summaryproj
						lines := strings.Split(output, "\n")
						found := false
						for _, line := range lines {
							if strings.Contains(line, "summaryproj") {
								found = true
								assertContains(t, line, "auto-complete:on")
								break
							}
						}
						if !found {
							t.Fatal("summaryproj not found in list output")
						}
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
