CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    statuses TEXT NOT NULL,
    transitions TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_workflows_name ON workflows(name);

INSERT INTO workflows (id, name, statuses, transitions) VALUES (
    '00000000-0000-0000-0000-000000000000',
    'kanban',
    '{"pending":["initial"],"active":["start","highlight"],"completed":["terminal","done","dim"],"deleted":["terminal","delete","dim"]}',
    '[["pending","active"],["active","pending"],["active","completed"],["completed","pending"],["pending","deleted"],["active","deleted"]]'
);
