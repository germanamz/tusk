ALTER TABLE tasks ADD COLUMN "order" REAL;

WITH numbered AS (
    SELECT
        id,
        CAST(ROW_NUMBER() OVER (
            PARTITION BY parent_id
            ORDER BY created_at ASC, id ASC
        ) AS REAL) AS seq
    FROM tasks
)
UPDATE tasks
SET "order" = (SELECT seq FROM numbered WHERE numbered.id = tasks.id);

CREATE INDEX idx_tasks_parent_order ON tasks(parent_id, "order");
