package e2e

import (
	"testing"
)

func TestPlayerRegistration(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "register_player",
			Steps: []Step{
				{
					Args: []string{"player", "register", "test-agent", "--type", "agent"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["id"], "test-agent")
						assertEqual(t, m["type"], "agent")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Registered")
						assertContains(t, output, "test-agent")
					},
				},
			},
		},
		{
			Name: "register_player_default_type",
			Steps: []Step{
				{
					Args: []string{"player", "register", "default-agent"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["type"], "agent")
					},
				},
			},
		},
		{
			Name: "register_player_human",
			Steps: []Step{
				{
					Args: []string{"player", "register", "german", "--type", "human"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["type"], "human")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestClaimRelease(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "claim_and_release",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1"}},
				{Args: []string{"task", "create", "Claimable task"}},
				{
					Args: []string{"task", "claim", "$1.short_id", "--player", "agent-1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["claimed_by"], "agent-1")
						if m["claimed_at"] == nil {
							t.Error("expected claimed_at to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Claimed")
					},
				},
				{
					Args: []string{"task", "release", "$1.short_id", "--player", "agent-1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["claimed_by"] != nil {
							t.Errorf("expected claimed_by to be nil after release, got %v", m["claimed_by"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Released")
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, "already claimed")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestStartAutoClaim(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "start_auto_claims",
			Steps: []Step{
				{Args: []string{"task", "create", "Auto-claim task"}},
				{
					Args: []string{"task", "start", "$0.short_id", "--player", "agent-auto"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
						assertEqual(t, m["claimed_by"], "agent-auto")
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
						if m["claimed_by"] != nil {
							t.Errorf("expected no claim, got %v", m["claimed_by"])
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, "already claimed")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestClaimVisibleInInfo(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "info_shows_claim",
			Steps: []Step{
				{Args: []string{"task", "create", "Visible claim task"}},
				{Args: []string{"task", "claim", "$0.short_id", "--player", "agent-vis"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["claimed_by"], "agent-vis")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Claimed By:")
						assertContains(t, output, "agent-vis")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestClaimPreservedAfterDone(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "done_preserves_claim",
			Steps: []Step{
				{Args: []string{"task", "create", "Finish task"}},
				{Args: []string{"task", "start", "$0.short_id", "--player", "agent-done"}},
				{
					Args: []string{"task", "done", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "completed")
						assertEqual(t, m["claimed_by"], "agent-done")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestPlayerModify(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "set_note_window_size",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1", "--type", "agent"}},
				{
					Args: []string{"player", "modify", "agent-1", "note-window-size=50"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["id"], "agent-1")
						if m["note_window_size"] == nil {
							t.Fatal("expected note_window_size to be set")
						}
						assertEqual(t, m["note_window_size"], float64(50))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Updated")
						assertContains(t, output, "note_window_size: 50")
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if _, ok := m["note_window_size"]; ok {
							t.Fatalf("expected note_window_size to be absent, got %v", m["note_window_size"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertNotContains(t, output, "note_window_size:")
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, "must be positive")
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, "must be an integer")
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, `unknown field "bogus"`)
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, "not found")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestClaimFilters(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "filter_claimed_by",
			Steps: []Step{
				{Args: []string{"task", "create", "Task A"}},
				{Args: []string{"task", "create", "Task B"}},
				{Args: []string{"task", "claim", "$0.short_id", "--player", "agent-filter"}},
				{
					Args: []string{"task", "list", "claimed_by=agent-filter"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						items := parsed.([]any)
						if len(items) != 1 {
							t.Fatalf("expected 1 task, got %d", len(items))
						}
						m := items[0].(map[string]any)
						assertEqual(t, m["claimed_by"], "agent-filter")
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						items := parsed.([]any)
						if len(items) != 1 {
							t.Fatalf("expected 1 unclaimed task, got %d", len(items))
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
