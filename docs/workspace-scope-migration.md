---
title: Migrating from per-project databases
---

# Migrating from per-project databases

Tusk v0.9 removes `[projects.<name>].db_path`. Every project declared in
a config file now shares the single database at `storage.path`. If you
had set `db_path` on one or more projects in an earlier version, follow
the manual migration below. Tusk ships no automatic converter — the
feature predates v0.1 and had no production users, but the steps are
documented here for completeness.

## 1. Inventory each per-project database

For every project that used its own file, point the old CLI at that
database and dump its tasks to JSON. `tusk list --format json` returns
every task in the active workspace, so run it once per file:

    TUSK_DB=/path/to/backend.db tusk list --format json > backend.json
    TUSK_DB=/path/to/frontend.db tusk list --format json > frontend.json

The JSON shape is:

    [
      {
        "short_id": "ec4cec3d",
        "project_id": "default",
        "title": "demo task",
        "description": "",
        "status": "pending",
        "priority": 3,
        ...
      }
    ]

Note two gaps:

- `tusk list` returns only tasks, not relations, annotations, or tag
  definitions. Capture those by running `tusk info <short_id>
  --format json` on each task you want to preserve.
- Timestamps (`created_at`, `modified_at`) cannot be preserved, because
  `tusk add` does not accept them as flags. The re-created tasks will
  carry the import time as their creation time.

## 2. Remove `db_path` from your config

Open your `~/.config/tusk/config.toml` and delete every
`db_path = ...` line under `[projects.<name>]`. Keep the workflow
binding and any urgency overrides. Viper ignores unknown keys on load,
but you should remove them so nobody is misled in the future.

## 3. Recreate each task in the workspace database

Run the new CLI with `storage.path` pointing at your consolidated
workspace file (the default is `~/.local/share/tusk/tusk.db`) and
recreate each task. A minimal jq recipe for a single file:

    jq -r '.[] | "tusk add \"\(.title)\" project=\(.project_id) priority=\(.priority)"' \
        backend.json | sh

Adjust the template to include any other fields you want to preserve
(`status=`, `parent=`, UDA keys). For relations and annotations,
re-attach them afterwards with `tusk link`, `tusk annotate`, and
`tusk tag` using the new short IDs the workspace store assigns.

## 4. Verify

    tusk list project=backend
    tusk list project=frontend

You should see the task set you had in the per-project files. Run
`tusk tree project=backend` to confirm any hierarchies you recreated,
and `tusk info <short_id>` on a handful of tasks to spot-check
relations and annotations.

## Notes

- Cross-project relations now work by construction — the old
  `ErrCrossStoreRelation` rejection is gone. If you had two tasks in
  separate per-project files that you wished you could link with
  `blocks`, you can do so after recreating them in the workspace DB.
- Short IDs are regenerated during `tusk add`, so the IDs in the new
  workspace will not match the originals. Update any scripts or notes
  that referenced the old IDs.
- Tags are deduped by name. Two per-project files that both defined a
  `bug` tag collapse to a single `bug` tag in the workspace store.
