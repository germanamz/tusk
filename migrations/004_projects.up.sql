CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT,
    settings TEXT NOT NULL DEFAULT '{}',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_projects_name ON projects(name);
CREATE INDEX idx_projects_workflow_id ON projects(workflow_id);

INSERT INTO projects (id, name, workflow_id) VALUES (
    '00000000-0000-0000-0000-000000000000',
    '_default',
    '00000000-0000-0000-0000-000000000000'
);
