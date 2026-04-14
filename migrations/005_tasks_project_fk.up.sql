CREATE TABLE tasks_new (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES tasks_new(id) ON DELETE SET NULL,
    project_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000'
        REFERENCES projects(id) ON DELETE RESTRICT,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    due_at TEXT,
    wait_until TEXT,
    recurrence_rule TEXT,
    uda TEXT DEFAULT '{}',
    created_at TEXT NOT NULL,
    modified_at TEXT NOT NULL,
    claimed_by TEXT REFERENCES players(id),
    claimed_at TEXT
);

INSERT INTO tasks_new
SELECT id, short_id, parent_id,
       '00000000-0000-0000-0000-000000000000',
       title, description, status, priority, version, due_at, wait_until,
       recurrence_rule, uda, created_at, modified_at, claimed_by, claimed_at
FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;

CREATE INDEX idx_tasks_short_id    ON tasks(short_id);
CREATE INDEX idx_tasks_parent_id   ON tasks(parent_id);
CREATE INDEX idx_tasks_project_id  ON tasks(project_id);
CREATE INDEX idx_tasks_status      ON tasks(status);
CREATE INDEX idx_tasks_due_at      ON tasks(due_at);
CREATE INDEX idx_tasks_wait_until  ON tasks(wait_until);
CREATE INDEX idx_tasks_claimed_by  ON tasks(claimed_by);
