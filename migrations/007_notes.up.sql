CREATE TABLE notes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    body TEXT NOT NULL,
    metadata TEXT NOT NULL DEFAULT '{}',
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_notes_project_player_created
    ON notes(project_id, player_id, created_at DESC);

CREATE INDEX idx_notes_task_id
    ON notes(task_id)
    WHERE task_id IS NOT NULL;
