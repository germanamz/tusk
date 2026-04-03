-- SQLite does not support DROP COLUMN prior to 3.35.0.
-- Recreate the table without the settings column.
CREATE TABLE projects_backup (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    default_workflow TEXT NOT NULL DEFAULT 'default',
    created_at TEXT NOT NULL
);

INSERT INTO projects_backup (id, name, description, default_workflow, created_at)
SELECT id, name, description, default_workflow, created_at FROM projects;

DROP TABLE projects;

ALTER TABLE projects_backup RENAME TO projects;
