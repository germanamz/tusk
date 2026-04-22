ALTER TABLE tasks ADD COLUMN level TEXT;
CREATE INDEX idx_tasks_level ON tasks(level);
