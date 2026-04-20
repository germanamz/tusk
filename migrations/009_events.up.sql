CREATE TABLE events (
    id          TEXT PRIMARY KEY,
    event_type  TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    entity_kind TEXT NOT NULL,
    player_id   TEXT,
    payload     TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE INDEX idx_events_created_at ON events(created_at);
CREATE INDEX idx_events_entity     ON events(entity_kind, entity_id, created_at);
CREATE INDEX idx_events_type       ON events(event_type, created_at);
