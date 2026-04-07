CREATE TABLE players (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('human', 'agent')),
    registered_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

ALTER TABLE tasks ADD COLUMN claimed_by TEXT REFERENCES players(id);
ALTER TABLE tasks ADD COLUMN claimed_at TEXT;

CREATE INDEX idx_tasks_claimed_by ON tasks(claimed_by);
