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

func TestNoteList(t *testing.T) {
	assertLen := func(t *testing.T, arr []any, want int) {
		t.Helper()
		if len(arr) != want {
			t.Fatalf("JSON length: got %d, want %d: %v", len(arr), want, arr)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						assertLen(t, arr, 2)
						for _, it := range arr {
							m := it.(map[string]any)
							assertEqual(t, m["player_id"], "alice")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if strings.Count(output, "●") != 2 {
							t.Fatalf("expected 2 bullets, output:\n%s", output)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						assertLen(t, arr, 3)
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if strings.Count(output, "●") != 3 {
							t.Fatalf("expected 3 bullets, output:\n%s", output)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						assertLen(t, arr, 1)
						m := arr[0].(map[string]any)
						assertEqual(t, m["player_id"], "bob")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "bob")
						assertNotContains(t, output, "alice-one")
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						assertLen(t, arr, 1)
						m := arr[0].(map[string]any)
						assertEqual(t, m["player_id"], "bob")
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "cannot be combined")
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						assertLen(t, arr, 2)
						a0 := arr[0].(map[string]any)["created_at"].(string)
						a1 := arr[1].(map[string]any)["created_at"].(string)
						if a0 < a1 {
							t.Fatalf("expected newest-first, got %q then %q", a0, a1)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						assertLen(t, arr, 0)
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "No notes.")
					},
				},
				{
					Args: []string{"note", "list", "project=default", "--archived", "--player", "alice"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						assertLen(t, arr, 1)
						m := arr[0].(map[string]any)
						if m["archived_at"] == nil || m["archived_at"] == "" {
							t.Fatalf("expected archived_at set, got %v", m["archived_at"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "[archived]")
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						assertLen(t, arr, 1)
						m := arr[0].(map[string]any)
						assertEqual(t, m["body"], "attached")
						if m["task_id"] == nil || m["task_id"] == "" {
							t.Fatalf("expected task_id set, got %v", m["task_id"])
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, `unknown field "bogus"`)
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "parsing --since")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}

func TestNoteListSince(t *testing.T) {
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"text", "json"},
	)
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		t.Run("since_filter/"+dbMode+"/"+format, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)

			if r := env.Run("note", "add", "old", "--player", "alice"); r.Err != nil {
				t.Fatalf("add old: %v\nstderr: %s", r.Err, r.Stderr)
			}
			if r := env.Run("note", "add", "new", "--player", "alice"); r.Err != nil {
				t.Fatalf("add new: %v\nstderr: %s", r.Err, r.Stderr)
			}

			backdateNoteByBody(t, env.dbPath, "old", time.Now().UTC().Add(-48*time.Hour))

			r := env.Run("note", "list", "project=default", "--since", "24h", "--player", "alice")
			if r.Err != nil {
				t.Fatalf("list: %v\nstderr: %s", r.Err, r.Stderr)
			}
			if format == "json" {
				var arr []any
				if err := json.Unmarshal([]byte(r.Stdout), &arr); err != nil {
					t.Fatalf("parse json: %v\nraw: %s", err, r.Stdout)
				}
				if len(arr) != 1 {
					t.Fatalf("expected 1 note, got %d: %s", len(arr), r.Stdout)
				}
				m := arr[0].(map[string]any)
				assertEqual(t, m["body"], "new")
			} else {
				assertContains(t, r.Stdout, "new")
				assertNotContains(t, r.Stdout, "old")
			}
		})
	}
}

func backdateNoteByBody(t *testing.T, dbPath, body string, when time.Time) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	res, err := db.Exec(
		`UPDATE notes SET created_at = ? WHERE body = ?`,
		when.UTC().Format("2006-01-02T15:04:05.000Z"),
		body,
	)
	if err != nil {
		t.Fatalf("update notes: %v", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected: %v", err)
	}
	if rows == 0 {
		t.Fatalf("no note with body %q to backdate", body)
	}
}
