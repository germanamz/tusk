-- Rename the built-in default project row from "_default" to "default" so the
-- service-layer path (which has a single "default" constant) does not need to
-- special-case name drift. Prior to Phase 3, SyncConfigToDB rewrote the name
-- at every startup via an UPDATE branch; Phase 3 removes that branch and makes
-- the DB row authoritative, so the rename has to happen once as a migration.
UPDATE projects
SET name = 'default'
WHERE id = '00000000-0000-0000-0000-000000000000'
  AND name = '_default';
