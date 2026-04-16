package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNoteAddArchive(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "add_with_metadata_then_archive",
			Steps: []Step{
				{
					Args: []string{"note", "add", "first note body", "meta.topic=planning", "--player", "tester"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["action"], "created")
						note := m["note"].(map[string]any)
						assertEqual(t, note["body"], "first note body")
						meta := note["metadata"].(map[string]any)
						assertEqual(t, meta["topic"], "planning")
						if _, has := note["task_id"]; has {
							t.Fatalf("expected no task_id, got %v", note["task_id"])
						}
						if _, has := note["archived_at"]; has {
							t.Fatalf("expected no archived_at on fresh note, got %v", note["archived_at"])
						}
						if note["id"] == "" || note["id"] == nil {
							t.Fatalf("expected id set, got %v", note["id"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created note")
					},
				},
				{
					Args: []string{"note", "archive", "$0.note.id", "--player", "tester"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["action"], "archived")
						note := m["note"].(map[string]any)
						if note["archived_at"] == nil || note["archived_at"] == "" {
							t.Fatalf("expected archived_at set, got %v", note["archived_at"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Archived note")
					},
				},
			},
		},
		{
			Name: "add_with_file_body",
			Steps: []Step{
				{
					Args: []string{"note", "add", "@./body.md", "project=default", "--player", "tester"},
					Setup: func(t *testing.T, dir string) string {
						t.Helper()
						path := filepath.Join(dir, "body.md")
						if err := os.WriteFile(path, []byte("# Heading\n\nSome body text."), 0o644); err != nil {
							t.Fatalf("write body.md: %v", err)
						}
						return dir
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						note := m["note"].(map[string]any)
						assertEqual(t, note["body"], "# Heading\n\nSome body text.")
					},
				},
			},
		},
		{
			Name: "add_with_stdin",
			Steps: []Step{
				{
					Args:  []string{"note", "add", "@-", "project=default", "--player", "tester"},
					Stdin: "stdin body\n",
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						note := m["note"].(map[string]any)
						assertEqual(t, note["body"], "stdin body")
					},
				},
			},
		},
		{
			Name: "add_missing_player_rejects",
			Steps: []Step{
				{
					Args:    []string{"note", "add", "body", "project=default"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "--player flag is required")
					},
				},
			},
		},
		{
			Name: "add_empty_body_rejects",
			Steps: []Step{
				{
					Args:    []string{"note", "add", "   ", "project=default", "--player", "tester"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "body must not be empty")
					},
				},
			},
		},
		{
			Name: "add_unknown_field_rejects",
			Steps: []Step{
				{
					Args:    []string{"note", "add", "body", "bogus=1", "project=default", "--player", "tester"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, `unknown field "bogus"`)
						assertStderrContains(t, r, "meta.")
					},
				},
			},
		},
		{
			Name: "archive_unknown_prefix_rejects",
			Steps: []Step{
				{
					Args:    []string{"note", "archive", "00000000", "--player", "tester"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "no note matches")
					},
				},
			},
		},
		{
			Name: "archive_non_author_rejects",
			Steps: []Step{
				{
					Args: []string{"note", "add", "owned body", "--player", "tester"},
				},
				{
					Args:    []string{"note", "archive", "$0.note.id", "--player", "intruder"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "archiving note")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
