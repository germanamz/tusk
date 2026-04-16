CREATE TABLE players_old (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('human', 'agent')),
    registered_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO players_old (id, type, registered_at, last_seen_at)
SELECT id, type, registered_at, last_seen_at FROM players;

DROP TABLE players;
ALTER TABLE players_old RENAME TO players;
