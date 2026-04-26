# Data Portability — Smoke Test Results

Run on 2026-04-26 against working tree atop commit 228e63a989545504d119d13330a13e464718fa0b
of branch `feat/data-portability-phase-4` (Phase 4 changes staged but not yet committed).

Each smoke ran against a temporary `--db <mktemp>` workspace under an isolated
`TUSK_CONFIG_DIR=$(mktemp -d)/empty-cfg` so it never touched the developer
workspace and never read a stale config.

## Smoke 1 — Full round-trip into an empty workspace

Result: PASS

Notes: Created three tasks; exported with `tusk export --output ws.json`;
rehydrated a fresh DB with `tusk import --input ws.json --replace --truncate`;
re-exported. `jq 'del(.exported_at) | .events |= map(select(.type !=
"workspace_imported"))'` over both files compared byte-equal under string
diff. Confirms IDs, timestamps, version numbers, and per-entity event
payloads all preserved through the codec.

## Smoke 2 — Stdin / stdout pipeline

Result: PASS

Notes: `tusk export | tusk --db <fresh> import --input - --replace
--truncate` succeeded; the destination workspace contained the expected 2
tasks. Validates the stdin TTY guard does not trip when stdin is a pipe.

## Smoke 3 — `--dry-run` on a real dump

Result: PASS

Notes: Counted tasks before (`task list | jq 'length'` → 2), ran `tusk
import --input ws.json --replace --dry-run`, counted after (→ 2).
Confirms validation runs but no writes land.

## Smoke 4 — Validation error surfaces cleanly

Result: PASS

Notes: Patched `parent_id` of the first task to a UUID not present in the
dump or workspace. `tusk import` exited non-zero and stderr contained the
`[fk]` issue tag. Validates the FK pre-validation pass and structured
ImportError rendering.

## Smoke 5 — Collision without `--replace` is rejected

Result: PASS

Notes: After a successful `--replace --truncate` import, repeating the
same import without `--replace` exited non-zero with `[collision]` tags
in stderr.

## Smoke 6 — `--replace --truncate` wipes and rehydrates

Result: PASS

Notes: Created a "garbage row that should disappear" in a destination DB,
then ran `tusk import --input ws.json --replace --truncate`. Subsequent
`task list | grep "garbage row"` returned no match, confirming the
pre-apply truncate reached every entity table.

## Smoke 7 — Schema-version mismatch path

Result: PASS

Notes: `jq '.schema_version = 999' ws.json` produced a stub; importing it
exited non-zero and stderr contained both `999` (the dump value) and
`supports 1` (the supported version) inside the `[schema]`-tagged issue.

## Smoke 8 — `workspace_imported` event lands once with the right counts

Result: PASS

Notes: After a clean `--replace --truncate` import, re-exporting the
destination workspace and filtering events by type produced exactly one
`workspace_imported` event. The payload's `counts` map matched the
applied entities — example: `{"annotations":0,"events":1,"notes":0,
"players":0,"projects":1,"relations":0,"tags":0,"tasks":1,
"workflows":1}`. Used `jq` against the re-exported JSON in lieu of
`sqlite3`, which is not present in the devcontainer.

---

## Phase-3 escape fixed during Phase 4

The round-trip smoke (and the equivalent e2e test in
`tests/e2e/portability_test.go`) initially failed on event payloads:
re-exported `task_created` events came back with empty `short_id`,
`title`, etc. Root cause: `sqlite.EventRepo.Record` calls
`json.Marshal(evt.Payload)`, and `domain.UnknownPayload`'s `Raw` field is
tagged `json:"-"`. Phase 3's `applyEvents` rehydrates every imported
event into an `UnknownPayload`, so re-recording stripped the original
payload bytes.

Fix: added `MarshalJSON` to `domain.UnknownPayload` so it emits the Raw
map (with `kind` injected if absent). Confirmed all `domain`, `sqlite`,
and `service` unit tests still pass with the change.
