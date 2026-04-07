-- Recreate tasks without claimed_by/claimed_at
CREATE TABLE tasks_backup AS SELECT
    id, short_id, parent_id, project_id, title, description,
    status, priority, version, due_at, wait_until, recurrence_rule, uda,
    created_at, modified_at
FROM tasks;

DROP TABLE tasks;

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    project_id TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    due_at TEXT,
    wait_until TEXT,
    recurrence_rule TEXT,
    uda TEXT DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    modified_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO tasks SELECT * FROM tasks_backup;
DROP TABLE tasks_backup;

CREATE INDEX idx_tasks_short_id ON tasks(short_id);
CREATE INDEX idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_due_at ON tasks(due_at);
CREATE INDEX idx_tasks_wait_until ON tasks(wait_until);

DROP TABLE players;
