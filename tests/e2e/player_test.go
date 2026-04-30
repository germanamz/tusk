package e2e

import (
	"testing"
)

func TestPlayerRegistration(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "register_player",
			Steps: []Step{
				{
					Args: []string{"player", "register", "test-agent", "--type", "agent"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["id"], "test-agent")
						assertEqual(test, m["type"], "agent")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Registered")
						assertContains(test, output, "test-agent")
					},
				},
			},
		},
		{
			Name: "register_player_default_type",
			Steps: []Step{
				{
					Args: []string{"player", "register", "default-agent"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["type"], "agent")
					},
				},
			},
		},
		{
			Name: "register_player_human",
			Steps: []Step{
				{
					Args: []string{"player", "register", "german", "--type", "human"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["type"], "human")
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

func TestClaimRelease(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "claim_and_release",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1"}},
				{Args: []string{"task", "create", "Claimable task"}},
				{
					Args: []string{"task", "claim", "$1.short_id", "--player", "agent-1"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["claimed_by"], "agent-1")
						if m["claimed_at"] == nil {
							test.Error("expected claimed_at to be set")
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Claimed")
					},
				},
				{
					Args: []string{"task", "release", "$1.short_id", "--player", "agent-1"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						if m["claimed_by"] != nil {
							test.Errorf("expected claimed_by to be nil after release, got %v", m["claimed_by"])
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Released")
					},
				},
			},
		},
		{
			Name: "claim_already_claimed",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1"}},
				{Args: []string{"player", "register", "agent-2"}},
				{Args: []string{"task", "create", "Contested task"}},
				{Args: []string{"task", "claim", "$2.short_id", "--player", "agent-1"}},
				{
					Args:    []string{"task", "claim", "$2.short_id", "--player", "agent-2"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertContains(test, result.Stderr, "already claimed")
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

func TestStartAutoClaim(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "start_auto_claims",
			Steps: []Step{
				{Args: []string{"task", "create", "Auto-claim task"}},
				{
					Args: []string{"task", "start", "$0.short_id", "--player", "agent-auto"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["status"], "active")
						assertEqual(test, m["claimed_by"], "agent-auto")
					},
				},
			},
		},
		{
			Name: "start_without_player_no_claim",
			Steps: []Step{
				{Args: []string{"task", "create", "No-claim task"}},
				{
					Args: []string{"task", "start", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["status"], "active")
						if m["claimed_by"] != nil {
							test.Errorf("expected no claim, got %v", m["claimed_by"])
						}
					},
				},
			},
		},
		{
			Name: "start_claimed_by_other_fails",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1"}},
				{Args: []string{"player", "register", "agent-2"}},
				{Args: []string{"task", "create", "Guarded task"}},
				{Args: []string{"task", "claim", "$2.short_id", "--player", "agent-1"}},
				{
					Args:    []string{"task", "start", "$2.short_id", "--player", "agent-2"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertContains(test, result.Stderr, "already claimed")
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

func TestClaimVisibleInInfo(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "info_shows_claim",
			Steps: []Step{
				{Args: []string{"task", "create", "Visible claim task"}},
				{Args: []string{"task", "claim", "$0.short_id", "--player", "agent-vis"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["claimed_by"], "agent-vis")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Claimed By:")
						assertContains(test, output, "agent-vis")
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

func TestClaimPreservedAfterDone(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "done_preserves_claim",
			Steps: []Step{
				{Args: []string{"task", "create", "Finish task"}},
				{Args: []string{"task", "start", "$0.short_id", "--player", "agent-done"}},
				{
					Args: []string{"task", "done", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["status"], "completed")
						assertEqual(test, m["claimed_by"], "agent-done")
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

func TestPlayerModify(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "set_note_window_size",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1", "--type", "agent"}},
				{
					Args: []string{"player", "modify", "agent-1", "note-window-size=50"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["id"], "agent-1")
						if m["note_window_size"] == nil {
							test.Fatal("expected note_window_size to be set")
						}
						assertEqual(test, m["note_window_size"], float64(50))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Updated")
						assertContains(test, output, "note_window_size: 50")
					},
				},
			},
		},
		{
			Name: "clear_note_window_size",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1", "--type", "agent"}},
				{Args: []string{"player", "modify", "agent-1", "note-window-size=50"}},
				{
					Args: []string{"player", "modify", "agent-1", "note-window-size="},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						if _, ok := m["note_window_size"]; ok {
							test.Fatalf("expected note_window_size to be absent, got %v", m["note_window_size"])
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertNotContains(test, output, "note_window_size:")
					},
				},
			},
		},
		{
			Name: "reject_negative_size",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1", "--type", "agent"}},
				{
					Args:    []string{"player", "modify", "agent-1", "note-window-size=-5"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertContains(test, result.Stderr, "must be positive")
					},
				},
			},
		},
		{
			Name: "reject_non_integer",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1", "--type", "agent"}},
				{
					Args:    []string{"player", "modify", "agent-1", "note-window-size=abc"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertContains(test, result.Stderr, "must be an integer")
					},
				},
			},
		},
		{
			Name: "reject_unknown_field",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1", "--type", "agent"}},
				{
					Args:    []string{"player", "modify", "agent-1", "bogus=1"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertContains(test, result.Stderr, `unknown field "bogus"`)
					},
				},
			},
		},
		{
			Name: "reject_missing_player",
			Steps: []Step{
				{
					Args:    []string{"player", "modify", "ghost", "note-window-size=50"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertContains(test, result.Stderr, "not found")
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

func TestClaimFilters(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "filter_claimed_by",
			Steps: []Step{
				{Args: []string{"task", "create", "Task A"}},
				{Args: []string{"task", "create", "Task B"}},
				{Args: []string{"task", "claim", "$0.short_id", "--player", "agent-filter"}},
				{
					Args: []string{"task", "list", "claimed_by=agent-filter"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						items := parsed.([]any)
						if len(items) != 1 {
							test.Fatalf("expected 1 task, got %d", len(items))
						}
						m := items[0].(map[string]any)
						assertEqual(test, m["claimed_by"], "agent-filter")
					},
				},
			},
		},
		{
			Name: "filter_unclaimed",
			Steps: []Step{
				{Args: []string{"task", "create", "Unclaimed task"}},
				{Args: []string{"task", "create", "Claimed task"}},
				{Args: []string{"task", "claim", "$1.short_id", "--player", "agent-unc"}},
				{
					Args: []string{"task", "list", "unclaimed=true"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						items := parsed.([]any)
						if len(items) != 1 {
							test.Fatalf("expected 1 unclaimed task, got %d", len(items))
						}
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
