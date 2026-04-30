package e2e

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestNoteAddArchive(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "add_with_metadata_then_archive",
			Steps: []Step{
				{
					Args: []string{"note", "add", "first note body", "meta.topic=planning", "--player", "tester"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["action"], "created")
						note := m["note"].(map[string]any)
						assertEqual(test, note["body"], "first note body")
						meta := note["metadata"].(map[string]any)
						assertEqual(test, meta["topic"], "planning")
						if _, has := note["task_id"]; has {
							test.Fatalf("expected no task_id, got %v", note["task_id"])
						}
						if _, has := note["archived_at"]; has {
							test.Fatalf("expected no archived_at on fresh note, got %v", note["archived_at"])
						}
						if note["id"] == "" || note["id"] == nil {
							test.Fatalf("expected id set, got %v", note["id"])
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Created note")
					},
				},
				{
					Args: []string{"note", "archive", "$0.note.id", "--player", "tester"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["action"], "archived")
						note := m["note"].(map[string]any)
						if note["archived_at"] == nil || note["archived_at"] == "" {
							test.Fatalf("expected archived_at set, got %v", note["archived_at"])
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Archived note")
					},
				},
			},
		},
		{
			Name: "add_with_file_body",
			Steps: []Step{
				{
					Args: []string{"note", "add", "@./body.md", "project=default", "--player", "tester"},
					Setup: func(test *testing.T, dir string) string {
						test.Helper()
						path := filepath.Join(dir, "body.md")
						if err := os.WriteFile(path, []byte("# Heading\n\nSome body text."), 0o644); err != nil {
							test.Fatalf("write body.md: %v", err)
						}
						return dir
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						note := m["note"].(map[string]any)
						assertEqual(test, note["body"], "# Heading\n\nSome body text.")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						note := m["note"].(map[string]any)
						assertEqual(test, note["body"], "stdin body")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "--player flag is required")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "body must not be empty")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, `unknown field "bogus"`)
						assertStderrContains(test, result, "meta.")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "no note matches")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "archiving note")
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}

func TestNoteList(test *testing.T) {
	assertLen := func(test *testing.T, arr []any, want int) {
		test.Helper()
		if len(arr) != want {
			test.Fatalf("JSON length: got %d, want %d: %v", len(arr), want, arr)
		}
	}

	scenarios := []Scenario{
		{
			Name: "own_notes_by_default",
			Steps: []Step{
				{Args: []string{"note", "add", "alice-one", "--player", "alice"}},
				{Args: []string{"note", "add", "alice-two", "--player", "alice"}},
				{Args: []string{"note", "add", "bob-one", "--player", "bob"}},
				{
					Args: []string{"note", "list", "project=default", "--player", "alice"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						assertLen(test, arr, 2)
						for _, it := range arr {
							m := it.(map[string]any)
							assertEqual(test, m["player_id"], "alice")
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						if strings.Count(output, "●") != 2 {
							test.Fatalf("expected 2 bullets, output:\n%s", output)
						}
					},
				},
			},
		},
		{
			Name: "all_players_shows_every_note",
			Steps: []Step{
				{Args: []string{"note", "add", "alice-one", "--player", "alice"}},
				{Args: []string{"note", "add", "alice-two", "--player", "alice"}},
				{Args: []string{"note", "add", "bob-one", "--player", "bob"}},
				{
					Args: []string{"note", "list", "project=default", "--all-players", "--player", "alice"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						assertLen(test, arr, 3)
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						if strings.Count(output, "●") != 3 {
							test.Fatalf("expected 3 bullets, output:\n%s", output)
						}
					},
				},
			},
		},
		{
			Name: "inline_player_filter",
			Steps: []Step{
				{Args: []string{"note", "add", "alice-one", "--player", "alice"}},
				{Args: []string{"note", "add", "bob-one", "--player", "bob"}},
				{
					Args: []string{"note", "list", "project=default", "player=bob", "--player", "alice"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						assertLen(test, arr, 1)
						m := arr[0].(map[string]any)
						assertEqual(test, m["player_id"], "bob")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "bob")
						assertNotContains(test, output, "alice-one")
					},
				},
			},
		},
		{
			Name: "filter_player_flag",
			Steps: []Step{
				{Args: []string{"note", "add", "alice-one", "--player", "alice"}},
				{Args: []string{"note", "add", "bob-one", "--player", "bob"}},
				{
					Args: []string{"note", "list", "project=default", "--filter-player", "bob", "--player", "alice"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						assertLen(test, arr, 1)
						m := arr[0].(map[string]any)
						assertEqual(test, m["player_id"], "bob")
					},
				},
			},
		},
		{
			Name: "all_players_with_filter_rejects",
			Steps: []Step{
				{Args: []string{"note", "add", "bob-one", "--player", "bob"}},
				{
					Args:    []string{"note", "list", "project=default", "--all-players", "--filter-player", "bob", "--player", "alice"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "cannot be combined")
					},
				},
			},
		},
		{
			Name: "window_override",
			Steps: []Step{
				{Args: []string{"note", "add", "one", "--player", "alice"}},
				{Args: []string{"note", "add", "two", "--player", "alice"}},
				{Args: []string{"note", "add", "three", "--player", "alice"}},
				{Args: []string{"note", "add", "four", "--player", "alice"}},
				{Args: []string{"note", "add", "five", "--player", "alice"}},
				{
					Args: []string{"note", "list", "project=default", "--window", "2", "--player", "alice"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						assertLen(test, arr, 2)
						a0 := arr[0].(map[string]any)["created_at"].(string)
						a1 := arr[1].(map[string]any)["created_at"].(string)
						if a0 < a1 {
							test.Fatalf("expected newest-first, got %q then %q", a0, a1)
						}
					},
				},
			},
		},
		{
			Name: "archived_toggle",
			Steps: []Step{
				{Args: []string{"note", "add", "archiveable", "--player", "alice"}},
				{Args: []string{"note", "archive", "$0.note.id", "--player", "alice"}},
				{
					Args: []string{"note", "list", "project=default", "--player", "alice"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						assertLen(test, arr, 0)
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "No notes.")
					},
				},
				{
					Args: []string{"note", "list", "project=default", "--archived", "--player", "alice"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						assertLen(test, arr, 1)
						m := arr[0].(map[string]any)
						if m["archived_at"] == nil || m["archived_at"] == "" {
							test.Fatalf("expected archived_at set, got %v", m["archived_at"])
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "[archived]")
					},
				},
			},
		},
		{
			Name: "task_scope",
			Steps: []Step{
				{Args: []string{"task", "create", "T-note"}},
				{Args: []string{"note", "add", "attached", "--task", "$0.short_id", "--player", "alice"}},
				{Args: []string{"note", "add", "detached", "--player", "alice"}},
				{
					Args: []string{"note", "list", "project=default", "--task", "$0.short_id", "--player", "alice"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						assertLen(test, arr, 1)
						m := arr[0].(map[string]any)
						assertEqual(test, m["body"], "attached")
						if m["task_id"] == nil || m["task_id"] == "" {
							test.Fatalf("expected task_id set, got %v", m["task_id"])
						}
					},
				},
			},
		},
		{
			Name: "unknown_bare_field_rejects",
			Steps: []Step{
				{
					Args:    []string{"note", "list", "project=default", "bogus=1", "--player", "alice"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, `unknown field "bogus"`)
					},
				},
			},
		},
		{
			Name: "invalid_since_rejects",
			Steps: []Step{
				{
					Args:    []string{"note", "list", "project=default", "--since", "bogus", "--player", "alice"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "parsing --since")
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}

func TestNoteListSince(test *testing.T) {
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"text", "json"},
	)
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		test.Run("since_filter/"+dbMode+"/"+format, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			if result := env.Run("note", "add", "old", "--player", "alice"); result.Err != nil {
				test.Fatalf("add old: %v\nstderr: %s", result.Err, result.Stderr)
			}
			if result := env.Run("note", "add", "new", "--player", "alice"); result.Err != nil {
				test.Fatalf("add new: %v\nstderr: %s", result.Err, result.Stderr)
			}

			backdateNoteByBody(test, env.dbPath, "old", time.Now().UTC().Add(-48*time.Hour))

			result := env.Run("note", "list", "project=default", "--since", "24h", "--player", "alice")
			if result.Err != nil {
				test.Fatalf("list: %v\nstderr: %s", result.Err, result.Stderr)
			}
			if format == "json" {
				var arr []any
				if parseErr := json.Unmarshal([]byte(result.Stdout), &arr); parseErr != nil {
					test.Fatalf("parse json: %v\nraw: %s", parseErr, result.Stdout)
				}
				if len(arr) != 1 {
					test.Fatalf("expected 1 note, got %d: %s", len(arr), result.Stdout)
				}
				m := arr[0].(map[string]any)
				assertEqual(test, m["body"], "new")
			} else {
				assertContains(test, result.Stdout, "new")
				assertNotContains(test, result.Stdout, "old")
			}
		})
	}
}

func backdateNoteByBody(test *testing.T, dbPath, body string, when time.Time) {
	test.Helper()
	db, openErr := sql.Open("sqlite", dbPath)

	if openErr != nil {
		test.Fatalf("open db: %v", openErr)
	}

	defer db.Close()
	res, execErr := db.Exec(
		`UPDATE notes SET created_at = ? WHERE body = ?`,
		when.UTC().Format("2006-01-02T15:04:05.000Z"),
		body,
	)

	if execErr != nil {
		test.Fatalf("update notes: %v", execErr)
	}

	rows, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		test.Fatalf("rows affected: %v", rowsErr)
	}

	if rows == 0 {
		test.Fatalf("no note with body %q to backdate", body)
	}
}
