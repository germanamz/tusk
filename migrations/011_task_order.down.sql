DROP INDEX IF EXISTS idx_tasks_parent_order;
ALTER TABLE tasks DROP COLUMN "order";
