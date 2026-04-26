# Data Portability — Phase 2: Codec (`internal/portability/`)

**Initiative:** v0.13 — Data Portability
**Spec:** `docs/superpowers/specs/2026-04-26-data-portability-design.md`
**Phase:** 2 of 4
**Prerequisites:** None — this phase runs against the base codebase on branch `feat/data-portability`.
**Can run in parallel with:** Phase 1 (foundations). Phase 1 touches `service/`, `sqlite/`, `domain/`, `client.go`. Phase 2 only adds files under `internal/portability/`. Zero overlap.

---

## Inherits From

This phase runs against the base codebase. It does **not** depend on Phase 1's `WriteTx` extension or `domain.EventWorkspaceImported` constant. Everything Phase 2 touches is new code under `internal/portability/`.

The codec deliberately does not import `domain` or `repository`. The DTOs are 1:1 with `domain` types but exist in a separate package so the wire format can drift from the in-memory shape without ripple changes through the service layer (e.g. for cleared-`order` round-trip; see Spec → JSON envelope → `tasks[].order`).

---

## Why this phase

The Data Portability initiative needs a JSON encoder/decoder over a neutral `PortableWorkspace` value before the service can serialize anything. The codec is pure — no DB knowledge, no business logic — so it can be built and tested in complete isolation.

The codec also owns the typed import-error envelope (`ImportIssue`, `ImportError`) so that the codec itself can produce structured errors when the JSON is malformed at the wire level (bad `schema_version`, malformed JSON shape). The service in Phase 3 reuses the same envelope for validation-pass errors.

---

## Tasks

### Task 1 — Create `internal/portability/portable.go` with `PortableWorkspace` and per-entity DTOs

**New file:** `internal/portability/portable.go`

Declare the package and the wire-shape types:

```go
// Package portability owns the neutral wire representation of a tusk
// workspace plus the JSON encoder and decoder. It has no dependency on
// the service or repository layers — codec consumers (CLI commands,
// the PortabilityService) are responsible for translating between the
// PortableWorkspace value and the live workspace.
package portability

import (
	"encoding/json"
	"time"
)

// SchemaVersion is the current portable wire-format version. Incremented
// on any breaking change to the PortableWorkspace shape (new required
// fields, removed fields, changed types, changed semantic meaning).
// Additive optional fields do not bump.
const SchemaVersion = 1

// PortableWorkspace is the root of an exported tusk workspace.
// Top-level lists are flat — entities reference each other by ID.
type PortableWorkspace struct {
	SchemaVersion int       `json:"schema_version"`
	TuskVersion   string    `json:"tusk_version"`
	ExportedAt    time.Time `json:"exported_at"`

	Workflows   []PortableWorkflow   `json:"workflows"`
	Projects    []PortableProject    `json:"projects"`
	Players     []PortablePlayer     `json:"players"`
	Tags        []PortableTag        `json:"tags"`
	Tasks       []PortableTask       `json:"tasks"`
	Relations   []PortableRelation   `json:"relations"`
	Annotations []PortableAnnotation `json:"annotations"`
	Notes       []PortableNote       `json:"notes"`
	Events      []PortableEvent      `json:"events"`
}
```

Then declare per-entity DTOs. **Each DTO is a 1:1 mirror of the corresponding `domain.*` type**, copying every exported field with matching JSON tags. Use the spec's "Field-shape decisions worth calling out" section as the authoritative reference for nullable encoding decisions:

- **`PortableTask`** — every `domain.Task` field. Field-shape rules:
  - `Order *float64 \`json:"order"\`` — encoded as JSON number or `null`, never omitted.
  - `Tags []string \`json:"tags"\`` — list of tag names assigned to this task; the authoritative tag rows live in `PortableWorkspace.Tags`.
  - `UDA map[string]string \`json:"uda"\`` — string-to-string only (matching the existing UDA contract).
  - `UrgencyOverrides *PortableUrgencyOverrides \`json:"urgency_overrides,omitempty"\`` — full struct mirroring `domain.UrgencyOverrides`, each field `*float64` with `omitempty`. Nil when no overrides.
  - `ClaimedBy *string \`json:"claimed_by,omitempty"\``, `ClaimedAt *time.Time \`json:"claimed_at,omitempty"\``.
  - All other fields use direct types matching `domain.Task` with `omitempty` on optional pointers.
  - **Excluded:** the transient computed fields `Urgency` and `EffectiveWeights` (read `domain/task.go:36-42` — both are documented as "not persisted", so they don't belong in the portable wire shape).
- **`PortableWorkflow`** — name, statuses (map of name → roles list), transitions (list of from/to pairs), version, timestamps. Mirror `domain.Workflow`. Use `[]string` for the roles list since `domain.StatusRole` is a string alias.
- **`PortableProject`** — id, name, workflow_id, settings (full `PortableProjectSettings` mirroring `domain.ProjectSettings`), version, timestamps.
- **`PortablePlayer`** — id, type, note_window_size, registered_at, last_seen_at.
- **`PortableTag`** — id, name, color (nullable). The `TagWithUsage` variant is **not** included; it's a read-time projection, not state.
- **`PortableRelation`** — id, source_id, target_id, relation_type, created_at.
- **`PortableAnnotation`** — id, task_id, body, created_at.
- **`PortableNote`** — id, project_id, player_id, task_id (nullable), body, metadata, archived_at (nullable), created_at.
- **`PortableEvent`** — id, type (string), entity_id, entity_kind (string), player_id (nullable), payload (`json.RawMessage` so unknown event types round-trip), created_at.

**On `PortableUrgencyOverrides` and `PortableProjectSettings`:** copy every field from the corresponding `domain` type with matching JSON tags. Both structs already use `*float64` / `*int` pointers and JSON tags in the domain package; mirror them exactly so encode and decode round-trip via the standard library.

Use `uuid.UUID` from `github.com/google/uuid` for ID fields — `encoding/json` handles that type via its `MarshalText`/`UnmarshalText`.

Do not import `domain` or `repository`. The DTOs deliberately re-declare every field so the codec can evolve independently of in-memory representations.

### Task 2 — Create `internal/portability/encode.go` with the JSON encoder

**New file:** `internal/portability/encode.go`

Expose a single function:

```go
// Encode writes ws as pretty-printed JSON to w. The output is UTF-8 with
// 2-space indentation; consumers can re-pipe through `jq -c` for a
// compact form. Returns any I/O or encoding error from the underlying
// json.Encoder.
func Encode(w io.Writer, ws *PortableWorkspace) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // tusk dumps are not HTML; preserve `&`, `<`, `>` literally
	return enc.Encode(ws)
}
```

`SetEscapeHTML(false)` matters for descriptions and notes that contain `<`, `>`, or `&` — the default `&lt;` escaping makes round-trips technically valid but visually confusing.

### Task 3 — Create `internal/portability/decode.go` with the decoder + error envelope

**New file:** `internal/portability/decode.go`

This file carries three things: the decoder, the typed error envelope, and the `schema_version` check.

```go
// ImportIssue is a single problem detected during import. Phases 2 and 3
// both produce these — Phase 2 (codec) issues come from malformed JSON
// or schema-version mismatches; Phase 3 (service) issues come from FK,
// taxonomy, cycle, and collision validation.
type ImportIssue struct {
	Kind        string `json:"kind"`         // "schema" | "json" | "taxonomy" | "fk" | "cycle" | "workflow" | "collision"
	EntityKind  string `json:"entity_kind"`  // "task" | "relation" | "project" | … | "" for codec-level issues
	EntityID    string `json:"entity_id"`    // UUID if present, short_id if known, "" if neither
	JSONPointer string `json:"json_pointer"` // e.g. "/tasks/42/parent_id" for codec-level errors; "" otherwise
	Message     string `json:"message"`      // one-line human message
}

// ImportError aggregates every issue detected during import. The
// validation pass collects all issues before returning so the user sees
// the full picture in one round-trip. ImportError satisfies the error
// interface; its Error() returns "import failed: <N> issues".
type ImportError struct {
	Issues []ImportIssue
}

func (e *ImportError) Error() string {
	return fmt.Sprintf("import failed: %d issues", len(e.Issues))
}

// Decode reads a JSON-encoded PortableWorkspace from r. It returns
// (*PortableWorkspace, nil) on success.
//
// Failure modes:
//   - Malformed JSON → returns *ImportError with one Kind="json" issue.
//     Wire shape errors (wrong type for a field, malformed UUID) end up
//     here.
//   - schema_version != SchemaVersion → returns *ImportError with one
//     Kind="schema" issue naming both the dump's value and the supported
//     value.
//
// Decode does not validate referential integrity, taxonomy, or cycles;
// those are the PortabilityService's responsibility.
func Decode(r io.Reader) (*PortableWorkspace, error) {
	var ws PortableWorkspace
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields() // future-proof: extra fields surface immediately
	if err := dec.Decode(&ws); err != nil {
		return nil, &ImportError{Issues: []ImportIssue{{
			Kind:    "json",
			Message: fmt.Sprintf("decode failed: %v", err),
		}}}
	}
	if ws.SchemaVersion != SchemaVersion {
		return nil, &ImportError{Issues: []ImportIssue{{
			Kind: "schema",
			Message: fmt.Sprintf(
				"unsupported schema_version %d (this tusk supports %d); the dump was produced by tusk %s",
				ws.SchemaVersion, SchemaVersion, ws.TuskVersion,
			),
		}}}
	}
	return &ws, nil
}
```

Note on `DisallowUnknownFields`: this is strict. If a future schema adds an optional field, the decoder rejects dumps that include it. That's fine for v0.13 because `schema_version` is the explicit forward-compat hatch — additive optional fields would still bump the schema version per the spec. If the strictness becomes painful later, relax it then.

### Task 4 — Round-trip and edge-case unit tests

**New file:** `internal/portability/encode_test.go`

Build a fully-populated `PortableWorkspace` in memory, encode it, decode the result, and assert deep equality. Cover at minimum:

1. **Happy path** — one of every entity kind, every field set to a non-zero value.
2. **Cleared `task.order`** — `Order = nil` round-trips as JSON `null` and decodes back to `nil`. Encode the JSON and assert the substring `"order":null` appears in the output (this is the v0.13 follow-up at ROADMAP.md:1305).
3. **Empty `urgency_overrides`** — `UrgencyOverrides = nil` round-trips as the field being omitted.
4. **Multi-line `description`** — task with a description containing `\n` round-trips byte-for-byte.
5. **Unset claim** — `ClaimedBy = nil`, `ClaimedAt = nil` round-trip as omitted fields.
6. **Set claim** — both `ClaimedBy` and `ClaimedAt` populated round-trip exactly.
7. **Empty UDA map** — `UDA = nil` and `UDA = map[string]string{}` both round-trip to consistent state (the test asserts which of the two the decoder produces, e.g. always `nil` after decode for an empty/missing map; pick one and document with a comment).
8. **Event with unknown type** — `PortableEvent.Type = "future_event_kind"` with arbitrary `json.RawMessage` payload round-trips losslessly (the codec doesn't dispatch on event type).
9. **HTML-special characters** — a task description containing `<`, `>`, `&` round-trips literally (verifies the `SetEscapeHTML(false)` setting).

Use `reflect.DeepEqual` on the decoded value vs the original, OR `cmp.Diff` if the project already uses `go-cmp`. Check for an existing pattern by looking at how `domain/task_test.go` and similar tests compare values; mirror that.

**New file:** `internal/portability/decode_test.go`

Cover the decoder error paths:

1. **Schema-version mismatch** — feed JSON with `schema_version: 999`; assert `Decode` returns an `*ImportError` with a single issue, `Kind == "schema"`, and the message contains both `999` and the supported version.
2. **Malformed JSON** — feed `{not valid json`; assert `Decode` returns an `*ImportError` with `Kind == "json"`.
3. **Unknown field** — feed valid JSON with an extra top-level key `"foo": 1`; assert `Decode` rejects it (validates the `DisallowUnknownFields` setting).
4. **Empty workspace** — feed minimal JSON (`{"schema_version": 1, "tusk_version": "v0.0.0", "exported_at": "..."}`) with all entity lists empty; assert `Decode` succeeds and returns a `PortableWorkspace` with empty (or nil) lists.

### Task 5 — Verify build and tests

Run:

```bash
make build
go test ./internal/portability/...
make vet
make lint
```

All four must succeed. The codec package is self-contained, so it should build and test cleanly without any other phase landing.

---

## Acceptance criteria

1. `make build` succeeds.
2. `go test ./internal/portability/...` passes.
3. `make vet` and `make lint` succeed.
4. `internal/portability/portable.go`, `encode.go`, `decode.go` exist and export the symbols listed above.
5. The codec package imports neither `domain` nor `repository` nor `service` nor `sqlite`.
6. **No behavior change visible to CLI or MCP users.** This phase adds dead code (no caller wired up); existing `tusk` commands work identically.

---

## User-visible behavior preserved

- All existing `tusk` CLI commands work identically.
- All existing MCP tools and resources work identically.
- The library `Client` API is unchanged.
- Build and existing test suite unchanged.

---

## Changes Introduced

**New files:**
- `internal/portability/portable.go` — `PortableWorkspace`, per-entity DTOs (`PortableTask`, `PortableWorkflow`, `PortableProject`, `PortablePlayer`, `PortableTag`, `PortableRelation`, `PortableAnnotation`, `PortableNote`, `PortableEvent`, `PortableUrgencyOverrides`, `PortableProjectSettings`), `SchemaVersion` constant.
- `internal/portability/encode.go` — `Encode(w io.Writer, ws *PortableWorkspace) error`.
- `internal/portability/decode.go` — `Decode(r io.Reader) (*PortableWorkspace, error)`, `ImportIssue`, `ImportError`.
- `internal/portability/encode_test.go` — round-trip and edge-case tests.
- `internal/portability/decode_test.go` — decoder error path tests.

**Modified files:** None.

**Modified interfaces:** None (new package, no existing consumer updated this phase).

**No new env vars, no schema migration, no new dependencies, no bridge code.**
